// Package app builds, runs and checks one command directory from what it
// finds there: a Go main, a package.json (Vite), gsx sources, a wrangler.toml
// (a Worker, one wasm per environment). mise names a stage per directory;
// this does the rest, so a new command is new lines in mise.toml, not new
// tooling.
package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sizes"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

// Dir is what one command directory contains.
type Dir struct {
	Path     string            // as given, e.g. cmd/gui
	Name     string            // its basename: the binary is bin/<Name>
	Root     string            // the repo root, where bin/ lives
	NPM      bool              // package.json: npm ci (when stale) and npm run build
	GSX      bool              // .gsx sources: go tool gsx generate
	Wrangler bool              // wrangler.toml: a Worker, wasm built per environment
	WasmOnly bool              // every Go file is js && wasm: no native binary
	Mains    map[string]string // wrangler environment → its main, "" for the top level
}

// Inspect reads a command directory.
func Inspect(path string) (Dir, error) {
	root, err := os.Getwd()
	if err != nil {
		return Dir{}, err
	}
	path = filepath.Clean(path)
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return Dir{}, fmt.Errorf("%s is not a directory", path)
	}
	d := Dir{Path: path, Name: filepath.Base(path), Root: root}
	d.NPM = exists(filepath.Join(path, "package.json"))
	gsx, _ := filepath.Glob(filepath.Join(path, "*.gsx"))
	d.GSX = len(gsx) > 0 || exists(filepath.Join(path, "gsx.toml"))
	if exists(filepath.Join(path, "wrangler.toml")) {
		d.Wrangler = true
		d.Mains, err = mains(filepath.Join(path, "wrangler.toml"))
		if err != nil {
			return d, err
		}
	}
	d.WasmOnly = wasmOnly(path)
	return d, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func mains(file string) (map[string]string, error) {
	var cfg struct {
		Main string `toml:"main"`
		Env  map[string]struct {
			Main string `toml:"main"`
		} `toml:"env"`
	}
	if _, err := toml.DecodeFile(file, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if cfg.Main == "" {
		return nil, fmt.Errorf("%s has no main", file)
	}
	m := map[string]string{"": cfg.Main}
	for name, e := range cfg.Env {
		m[name] = cfg.Main
		if e.Main != "" {
			m[name] = e.Main
		}
	}
	return m, nil
}

// wasmOnly reports whether the directory's Go files are all behind js && wasm,
// as a Worker entry point's are.
func wasmOnly(dir string) bool {
	cmd := exec.Command("go", "list", "-f", "{{.Name}}", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return err != nil && strings.Contains(string(out), "build constraints exclude all Go files")
}

func run(out io.Writer, dir string, env []string, name string, args ...string) error {
	fmt.Fprintf(out, "$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s (in %s): %w", name, strings.Join(args, " "), dir, err)
	}
	return nil
}

// Build builds what the directory holds. For a Worker, env picks the wrangler
// environment; its main's directory is the output directory, and one named
// tinygo is built with TinyGo, any other with Go.
func Build(out io.Writer, path, env string) error {
	d, err := Inspect(path)
	if err != nil {
		return err
	}
	if d.NPM {
		if stale(filepath.Join(d.Path, "package-lock.json"), filepath.Join(d.Path, "node_modules", ".package-lock.json")) {
			if err := run(out, d.Path, nil, "npm", "ci"); err != nil {
				return err
			}
		}
		if err := run(out, d.Path, nil, "npm", "run", "build"); err != nil {
			return err
		}
	}
	if d.GSX {
		if err := run(out, d.Path, nil, "go", "tool", "gsx", "generate", "-q"); err != nil {
			return err
		}
	}
	if !d.WasmOnly {
		if err := run(out, d.Path, nil, "go", "build", "-o", filepath.Join(d.Root, "bin", d.Name), "."); err != nil {
			return err
		}
	}
	if d.Wrangler {
		main, ok := d.Mains[env]
		if !ok {
			return fmt.Errorf("%s/wrangler.toml has no environment %q", d.Path, env)
		}
		outDir := filepath.Dir(main)
		mode, goEnv := "go", []string{"GOOS=js", "GOARCH=wasm"}
		if filepath.Base(outDir) == "tinygo" {
			mode = "tinygo"
		}
		if err := run(out, d.Path, nil, "go", "run", "github.com/syumai/workers-go/cmd/workers-assets-gen", "-mode="+mode, "-o", outDir); err != nil {
			return err
		}
		wasm := filepath.Join(outDir, "app.wasm")
		if mode == "tinygo" {
			err = run(out, d.Path, nil, "tinygo", "build", "-o", wasm, "-target", "wasm", "-no-debug", ".")
		} else {
			err = run(out, d.Path, goEnv, "go", "build", "-o", wasm, ".")
		}
		if err != nil {
			return err
		}
		raw, gz, err := sizes.Measure(filepath.Join(d.Path, wasm))
		if err != nil {
			return err
		}
		fmt.Fprint(out, sizes.Line(filepath.Join(d.Path, wasm), raw, gz))
	}
	return nil
}

// stale reports whether output is missing or older than input.
func stale(input, output string) bool {
	in, err := os.Stat(input)
	if err != nil {
		return false
	}
	o, err := os.Stat(output)
	return err != nil || o.ModTime().Before(in.ModTime())
}

// Run runs the directory's binary under fnox with args, replacing this
// process so signals reach it directly; or, as a Worker, wrangler dev in the
// directory.
func Run(path string, asWorker bool, env string, args []string) error {
	d, err := Inspect(path)
	if err != nil {
		return err
	}
	if asWorker {
		if !d.Wrangler {
			return fmt.Errorf("%s has no wrangler.toml; it is not a Worker", d.Path)
		}
		return execIn(d.Path, "wrangler", append([]string{"dev", "--env", env}, args...)...)
	}
	if d.WasmOnly {
		return fmt.Errorf("%s builds only a Worker; run it with --worker", d.Path)
	}
	return execIn(d.Root, "fnox", append([]string{"exec", "--", filepath.Join("bin", d.Name)}, args...)...)
}

// Check checks the directory: gsx formatting, go vet and go test (vet only,
// for wasm, where tests cannot run), a Worker's smoke round trip on workerd
// requesting reqPath, and the browser probe when the directory ships one.
func Check(out io.Writer, path, reqPath, expect string) error {
	d, err := Inspect(path)
	if err != nil {
		return err
	}
	if d.GSX {
		if err := run(out, d.Path, nil, "go", "tool", "gsx", "fmt", "-l", "."); err != nil {
			return fmt.Errorf("unformatted .gsx above; fix with: go tool gsx fmt -w %s", d.Path)
		}
	}
	var goEnv []string
	if d.WasmOnly {
		goEnv = []string{"GOOS=js", "GOARCH=wasm"}
	}
	if err := run(out, d.Path, goEnv, "go", "vet", "./..."); err != nil {
		return err
	}
	if !d.WasmOnly {
		if err := run(out, d.Path, nil, "go", "test", "./..."); err != nil {
			return err
		}
	}
	if d.Wrangler {
		if err := worker.Smoke(out, d.Path, "", reqPath, expect, 3*time.Minute); err != nil {
			return err
		}
	}
	if probe := filepath.Join(d.Path, "tools", "browser-check.mjs"); exists(probe) {
		if err := browser.Check(out, filepath.Join("bin", d.Name), probe, reqPath); err != nil {
			return err
		}
	}
	return nil
}
