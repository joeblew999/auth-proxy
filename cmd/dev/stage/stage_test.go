package stage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReadsWhatADirectoryHolds(t *testing.T) {
	t.Chdir(t.TempDir())
	os.MkdirAll("cmd/gui", 0o755)
	os.WriteFile("go.mod", []byte("module x\n\ngo 1.27\n"), 0o644)
	os.WriteFile("cmd/gui/main.go", []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile("cmd/gui/package.json", []byte("{}"), 0o644)
	os.WriteFile("cmd/gui/app.gsx", []byte(""), 0o644)
	os.WriteFile("cmd/gui/wrangler.toml", []byte("name = \"gui\"\nmain = \"build/go/worker.mjs\"\n[env.tinygo]\nmain = \"build/tinygo/worker.mjs\"\n[env.live]\nroutes = []\n"), 0o644)
	d, err := Inspect("cmd/gui")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "gui" || !d.NPM || !d.GSX || !d.Wrangler || d.WasmOnly {
		t.Fatalf("got %+v", d)
	}
	want := map[string]string{"": "build/go/worker.mjs", "tinygo": "build/tinygo/worker.mjs", "live": "build/go/worker.mjs"}
	for env, main := range want {
		if d.Mains[env] != main {
			t.Errorf("env %q: got %q, want %q", env, d.Mains[env], main)
		}
	}

	os.MkdirAll("cmd/worker", 0o755)
	os.WriteFile("cmd/worker/main.go", []byte("//go:build js && wasm\n\npackage main\n\nfunc main() {}\n"), 0o644)
	w, err := Inspect("cmd/worker")
	if err != nil {
		t.Fatal(err)
	}
	if !w.WasmOnly || w.NPM || w.GSX || w.Wrangler {
		t.Fatalf("worker: got %+v", w)
	}
	if _, err := Inspect("cmd/nothing"); err == nil {
		t.Fatal("a missing directory was accepted")
	}
}

func TestStale(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "lock"), filepath.Join(dir, "installed")
	os.WriteFile(in, nil, 0o644)
	if !stale(in, out) {
		t.Fatal("a missing output is not stale")
	}
	os.WriteFile(out, nil, 0o644)
	if stale(in, out) {
		t.Fatal("a fresh output is stale")
	}
	if stale(filepath.Join(dir, "no-input"), out) {
		t.Fatal("no input means nothing to do, not stale")
	}
}
