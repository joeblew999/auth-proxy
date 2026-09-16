// Package deps runs go-mod-upgrade in every Go module of the repo, so a
// dependency bump never misses a nested module. It runs as `dev deps ...`.
package deps

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

const usage = `dev deps list      list available Go module upgrades in every module, changing nothing
dev deps upgrade   interactively upgrade Go modules in every module
`

// Usage is the help text cmd/dev prints for these commands.
func Usage() string { return usage }

// Run dispatches the deps subcommand. Args are everything after "deps".
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 || (args[0] != "list" && args[0] != "upgrade") {
		fmt.Fprint(stderr, usage)
		return errors.New("usage: dev deps list|upgrade")
	}
	dirs, err := Modules(".")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		fmt.Fprintf(stdout, "== %s ==\n", dir)
		cmd := exec.Command("go-mod-upgrade")
		if args[0] == "list" {
			cmd.Args = append(cmd.Args, "--list")
		}
		cmd.Dir = dir
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: go-mod-upgrade: %w", dir, err)
		}
	}
	return nil
}

// skipped are directories that never hold a module of ours.
var skipped = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "bin": true}

// Modules lists every directory under root holding a go.mod, root first.
func Modules(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != root && skipped[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			dirs = append(dirs, filepath.Dir(p))
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs, err
}
