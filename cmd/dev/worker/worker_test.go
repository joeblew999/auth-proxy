package worker

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/fnox"
)

func TestWorkerNameFollowsWranglerRules(t *testing.T) {
	var cfg wranglerConfig
	if _, err := toml.Decode(`
name = "app"
[env.tinygo]
main = "x.mjs"
[env.live]
name = "app-live"
`, &cfg); err != nil {
		t.Fatal(err)
	}
	for env, want := range map[string]string{"": "app", "tinygo": "app-tinygo", "live": "app-live", "other": "app-other"} {
		if got := workerName(cfg, env); got != want {
			t.Errorf("env %q: got %q, want %q", env, got, want)
		}
	}
}

// fakeAccount serves the subdomain endpoint and counts the calls.
func fakeAccount(t *testing.T, token, subdomain string) (calls *int) {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"Invalid API token"}]}`)
			return
		}
		fmt.Fprintf(w, `{"success":true,"result":{"subdomain":%q}}`, subdomain)
	}))
	t.Cleanup(srv.Close)
	old := subdomainEndpoint
	subdomainEndpoint = srv.URL + "/accounts/%s/workers/subdomain"
	t.Cleanup(func() { subdomainEndpoint = old })
	return &n
}

func stubFnox(t *testing.T, values map[string]string) {
	t.Helper()
	old := fnox.Get
	fnox.Get = func(name string) (string, error) {
		v, ok := values[name]
		if !ok {
			return "", fmt.Errorf("fnox: %s not found", name)
		}
		return v, nil
	}
	t.Cleanup(func() { fnox.Get = old })
}

func TestURLReadsTheSubdomainOnceAndKeepsIt(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(wranglerFile, []byte("name = \"app\"\n[env.tinygo]\nmain = \"x.mjs\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := fakeAccount(t, "tok", "someone")
	stubFnox(t, map[string]string{"CLOUDFLARE_API_TOKEN": "tok", "CLOUDFLARE_ACCOUNT_ID": "acct"})

	if got, _ := URL(".", "", false, "http://127.0.0.1:1", false); got != "http://127.0.0.1:1" {
		t.Fatalf("local: got %q", got)
	}
	got, err := URL(".", "", true, "", false)
	if err != nil || got != "https://app.someone.workers.dev" {
		t.Fatalf("worker: got %q, %v", got, err)
	}
	got, err = URL(".", "tinygo", true, "", false)
	if err != nil || got != "https://app-tinygo.someone.workers.dev" {
		t.Fatalf("tinygo: got %q, %v", got, err)
	}
	if *calls != 1 {
		t.Fatalf("API asked %d times, want once", *calls)
	}
	data, _ := os.ReadFile(localFile)
	if !strings.Contains(string(data), "CLOUDFLARE_WORKERS_SUBDOMAIN = \"someone\"") {
		t.Fatalf("mise.local.toml:\n%s", data)
	}
	if _, err := URL(".", "", true, "", true); err != nil || *calls != 2 {
		t.Fatalf("refresh: %v, calls %d", err, *calls)
	}
}

func TestURLNamesTheMissingCredential(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile(wranglerFile, []byte("name = \"app\"\n"), 0o644)
	stubFnox(t, map[string]string{})
	_, err := URL(".", "", true, "", false)
	want := "CLOUDFLARE_API_TOKEN is not in fnox; store it with: fnox set -g CLOUDFLARE_API_TOKEN"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

func TestURLReportsWhatCloudflareSaid(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile(wranglerFile, []byte("name = \"app\"\n"), 0o644)
	fakeAccount(t, "right", "x")
	stubFnox(t, map[string]string{"CLOUDFLARE_API_TOKEN": "wrong", "CLOUDFLARE_ACCOUNT_ID": "acct"})
	_, err := URL(".", "", true, "", false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403: Invalid API token") {
		t.Fatalf("got %v", err)
	}
}

func TestCreatedReportsOnlyIDsWrittenBack(t *testing.T) {
	before := []byte(`name = "app"
kv_namespaces = [{ binding = "A" }, { binding = "B", id = "old" }]
[env.tinygo]
kv_namespaces = [{ binding = "A" }]
`)
	after := []byte(`name = "app"
kv_namespaces = [{ binding = "A", id = "new1" }, { binding = "B", id = "old" }]
[env.tinygo]
kv_namespaces = [{ binding = "A", id = "new2" }]
`)
	got, err := created(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kv_namespaces A id=new1", "kv_namespaces A id=new2 (env tinygo)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
	if same, _ := created(before, before); len(same) != 0 {
		t.Fatalf("unchanged config reported %q", same)
	}
}

func TestWait(t *testing.T) {
	oldSleep := sleep
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = oldSleep })
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := Wait(&out, srv.URL, time.Minute); err != nil {
		t.Fatal(err)
	}
	if n != 2+stableFor || strings.Count(out.String(), "waiting for") != 2 {
		t.Fatalf("n=%d out=%q", n, out.String())
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(530) }))
	defer down.Close()
	if err := Wait(&out, down.URL, 0); err == nil || !strings.Contains(err.Error(), "did not answer 200") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckJudgesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "<h1>Pick a model</h1>")
		case "/down":
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, "error code: 1042")
		}
	}))
	defer srv.Close()
	if code, _, err := check(srv.URL+"/ok", "Pick a model"); err != nil || code != 200 {
		t.Fatalf("ok: %d %v", code, err)
	}
	if _, _, err := check(srv.URL+"/ok", "Nope"); err == nil || !strings.Contains(err.Error(), `does not contain "Nope"`) {
		t.Fatalf("missing text: %v", err)
	}
	if _, _, err := check(srv.URL+"/down", ""); err == nil || !strings.Contains(err.Error(), "answered 502: error code: 1042") {
		t.Fatalf("down: %v", err)
	}
}

func TestWaitReadyReadsTheLog(t *testing.T) {
	oldSleep := sleep
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = oldSleep })
	log := filepath.Join(t.TempDir(), "dev.log")
	os.WriteFile(log, []byte("Starting local server...\nReady on http://127.0.0.1:8787\n"), 0o644)
	if err := waitReady(log, time.Minute); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(log, []byte("ERROR: build failed\n"), 0o644)
	if err := waitReady(log, time.Minute); err == nil {
		t.Fatal("an ERROR line was not reported")
	}
	os.WriteFile(log, []byte("still starting\n"), 0o644)
	if err := waitReady(log, 0); err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestHasBindings(t *testing.T) {
	if hasBindings([]byte("name = \"app\"\n")) {
		t.Fatal("a config with no bindings reported some")
	}
	if !hasBindings([]byte("name = \"app\"\nkv_namespaces = [{ binding = \"A\" }]\n")) {
		t.Fatal("a kv binding was not seen")
	}
	if !hasBindings([]byte("name = \"app\"\n[env.x]\nr2_buckets = [{ binding = \"B\" }]\n")) {
		t.Fatal("an env binding was not seen")
	}
}
