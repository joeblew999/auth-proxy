package secrets

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/fnox"
)

func stubFnox(t *testing.T, values map[string]string) (stored *[]string, pushed *[]string) {
	t.Helper()
	oldGet, oldSet, oldExec := fnox.Get, fnox.Set, fnox.Exec
	var st, pu []string
	fnox.Get = func(name string) (string, error) {
		v, ok := values[name]
		if !ok {
			return "", fmt.Errorf("fnox: %s not found", name)
		}
		return v, nil
	}
	fnox.Set = func(name, value string) error { st = append(st, name+"="+value); return nil }
	fnox.Exec = func(dir string, stdin io.Reader, _ io.Writer, args ...string) error {
		var buf bytes.Buffer
		buf.ReadFrom(stdin)
		pu = append(pu, dir+": "+strings.Join(args, " ")+" <- "+buf.String())
		return nil
	}
	t.Cleanup(func() { fnox.Get, fnox.Set, fnox.Exec = oldGet, oldSet, oldExec })
	return &st, &pu
}

func TestResolveMapsOwnersToSecrets(t *testing.T) {
	names := "ADMIN_API_KEY\tadmin\nXAI_API_KEY\txai\n"
	for arg, want := range map[string]string{"admin": "ADMIN_API_KEY", "xai": "XAI_API_KEY", "XAI_API_KEY": "XAI_API_KEY"} {
		if got, err := Resolve(names, arg); err != nil || got != want {
			t.Errorf("%q: got %q, %v", arg, got, err)
		}
	}
	if _, err := Resolve(names, "groq"); err == nil || !strings.Contains(err.Error(), "the secrets are: ADMIN_API_KEY, XAI_API_KEY") {
		t.Fatalf("unknown owner: %v", err)
	}
	if got, _ := Resolve("", "ANY"); got != "ANY" {
		t.Fatalf("no list: got %q", got)
	}
}

func TestPushNamesTheFixForEachMissingSecret(t *testing.T) {
	_, pushed := stubFnox(t, map[string]string{"A": "va", "B": ""})
	var out bytes.Buffer
	err := Push(strings.NewReader("A\tprov-a\nB\tprov-b\n\nC\n"), &out, "cmd/w", "tinygo", "mise run secrets:set {provider}")
	if err == nil || err.Error() != "2 secret(s) not pushed" {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"pushed  A", "missing B -> mise run secrets:set prov-b", "missing C -> mise run secrets:set C"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(*pushed) != 1 || (*pushed)[0] != "cmd/w: wrangler secret put A --env tinygo <- va" {
		t.Fatalf("pushed %q", *pushed)
	}
}

func TestSet(t *testing.T) {
	stored, pushed := stubFnox(t, map[string]string{})
	var out bytes.Buffer
	if err := Set(strings.NewReader(""), &out, &out, "K", true, false, ".", ""); err != nil {
		t.Fatal(err)
	}
	if len(*stored) != 1 || len((*stored)[0]) != len("K=")+64 {
		t.Fatalf("generated: stored %q", *stored)
	}
	if len(*pushed) != 1 || !strings.HasPrefix((*pushed)[0], ".: wrangler secret put K --env  <- ") {
		t.Fatalf("pushed %q", *pushed)
	}
	if err := Set(strings.NewReader("typed\n"), &out, &out, "P", false, false, ".", ""); err != nil || (*stored)[1] != "P=typed" {
		t.Fatalf("piped: %v %q", err, *stored)
	}
	if err := Set(strings.NewReader("\n"), &out, &out, "E", false, false, ".", ""); err == nil || !strings.Contains(err.Error(), "no value given for E") {
		t.Fatalf("empty: %v", err)
	}
	stored, _ = stubFnox(t, map[string]string{"HAVE": "x"})
	out.Reset()
	if err := Set(strings.NewReader(""), &out, &out, "HAVE", true, true, ".", ""); err != nil || len(*stored) != 0 {
		t.Fatalf("if-missing: %v, stored %q", err, *stored)
	}
	if !strings.Contains(out.String(), "HAVE is already in fnox") {
		t.Fatalf("if-missing output: %s", out.String())
	}
}
