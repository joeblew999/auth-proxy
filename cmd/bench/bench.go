// Command bench runs this proxy's Go and TinyGo Worker builds side by side in
// local workerd, both against the mock upstream, sends them identical requests
// and prints status, size and latency in one table. Everything it starts is
// stopped on exit. It is this project's, not the stack's: it knows the proxy's
// endpoints, so it lives beside them rather than in cmd/dev.
//
// workers-go instantiates the wasm module on every request, so latency
// includes that startup cost, which is where the two toolchains differ most.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sizes"
)

const (
	goPort     = 8791
	tinygoPort = 8792
	clientKey  = "bench-key"
	workerDir  = "cmd/worker" // where wrangler.toml and the builds live
	mockConfig = "cmd/mock-upstream/providers.toml"
	mockBinary = "bin/mock-upstream"
)

var wasm = []string{workerDir + "/build/go/app.wasm", workerDir + "/build/tinygo/app.wasm"}

type request struct{ name, method, path, body string }

var requests = []request{
	{"GET /health", http.MethodGet, "/health", ""},
	{"GET /admin/status", http.MethodGet, "/admin/status", ""},
	{"GET /v1/models", http.MethodGet, "/v1/models", ""},
	{"POST chat (default)", http.MethodPost, "/v1/chat/completions", `{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`},
	{"POST chat local/", http.MethodPost, "/v1/chat/completions", `{"model":"local/mock-model","messages":[{"role":"user","content":"hi"}]}`},
	{"POST chat stream", http.MethodPost, "/v1/chat/completions", `{"model":"mock-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`},
	{"POST /mcp", http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run does the whole comparison. RUNS in the environment sets the sample size (20).
func run(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: bench (RUNS=n in the environment changes the sample size)")
	}
	runs := 20
	if v := os.Getenv("RUNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return fmt.Errorf("RUNS must be a positive number, got %q", v)
		}
		runs = n
	}
	if _, err := os.Stat(mockBinary); err != nil {
		return fmt.Errorf("%s is missing; build it with: mise run build:mock", mockBinary)
	}
	providers, err := os.ReadFile(mockConfig)
	if err != nil {
		return err
	}

	logs, err := os.MkdirTemp("", "bench")
	if err != nil {
		return err
	}
	defer os.RemoveAll(logs)
	var started []*exec.Cmd
	defer func() {
		for _, c := range started {
			stop(c)
		}
	}()
	start := func(log, dir, name string, arg ...string) error {
		f, err := os.Create(filepath.Join(logs, log+".log"))
		if err != nil {
			return err
		}
		cmd := exec.Command(name, arg...)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = f, f
		ownGroup(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting %s: %w", name, err)
		}
		started = append(started, cmd)
		return nil
	}

	vars := []string{
		"--var", "PROVIDERS_TOML:" + string(providers),
		"--var", "MOCK_API_KEY:mock-key",
		"--var", "ADMIN_API_KEY:" + clientKey,
	}
	mock, err := filepath.Abs(mockBinary)
	if err != nil {
		return err
	}
	if err := start("mock", ".", mock); err != nil {
		return err
	}
	// wrangler dev finds the Worker's own wrangler.toml by running in its directory.
	if err := start("go", workerDir, "wrangler", append([]string{"dev", "--env", "", "--port", strconv.Itoa(goPort)}, vars...)...); err != nil {
		return err
	}
	if err := start("tinygo", workerDir, "wrangler", append([]string{"dev", "--env", "tinygo", "--port", strconv.Itoa(tinygoPort)}, vars...)...); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Building and starting both Workers (this takes a minute)...")
	for _, name := range []string{"go", "tinygo"} {
		if err := waitReady(stdout, filepath.Join(logs, name+".log"), 5*time.Minute); err != nil {
			return err
		}
	}

	fmt.Fprintln(stdout)
	for _, f := range wasm {
		raw, gz, err := sizes.Measure(f)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, sizes.Line(f, raw, gz))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "latency: mean of %d requests\n", runs)
	fmt.Fprintf(stdout, "%-20s | %-28s | %-28s\n", "request", "go   code    size   latency", "tinygo   code    size   latency")
	for _, r := range requests {
		g, err := sample(goPort, r, runs)
		if err != nil {
			return err
		}
		t, err := sample(tinygoPort, r, runs)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%-20s | %4d %7d B %8.1f ms | %4d %7d B %8.1f ms\n",
			r.name, g.code, g.size, g.meanMS, t.code, t.size, t.meanMS)
	}
	return nil
}

// waitReady watches a wrangler dev log for readiness or an error.
func waitReady(out io.Writer, log string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		data, _ := os.ReadFile(log)
		if bytes.Contains(data, []byte("ERROR")) {
			fmt.Fprint(out, string(data))
			return fmt.Errorf("wrangler dev failed; its log is above")
		}
		if bytes.Contains(data, []byte("Ready on")) {
			return nil
		}
		if time.Now().After(deadline) {
			fmt.Fprint(out, string(data))
			return fmt.Errorf("wrangler dev did not become ready within %s; its log is above", timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

type result struct {
	code   int
	size   int64
	meanMS float64
}

// sample sends one request runs times and reports the first response's status
// and size with the mean latency.
func sample(port int, r request, runs int) (result, error) {
	var res result
	var total time.Duration
	for i := 0; i < runs; i++ {
		code, size, dur, err := probe(port, r)
		if err != nil {
			return res, fmt.Errorf("%s on port %d: %w", r.name, port, err)
		}
		if i == 0 {
			res.code, res.size = code, size
		}
		total += dur
	}
	res.meanMS = float64(total.Microseconds()) / 1000 / float64(runs)
	return res, nil
}

func probe(port int, r request) (code int, size int64, dur time.Duration, err error) {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequest(r.method, fmt.Sprintf("http://127.0.0.1:%d%s", port, r.path), body)
	if err != nil {
		return 0, 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+clientKey)
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	begin := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return 0, 0, 0, err
	}
	return resp.StatusCode, n, time.Since(begin), nil
}
