package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Skills live in the repo so every Claude Code session here loads them, and are
// pinned so they never drift from the tools they describe.
const (
	skillsDir = ".claude/skills"
	lockFile  = "SKILLS.lock"

	// gsx skills come from the gsx module at the version this module pins, so
	// the guidance always matches the gsx that builds the app.
	gsxModule = "github.com/gsxhq/gsx"
	// gsxModuleDir is the module whose go.mod pins gsx.
	gsxModuleDir = "spikes/hello-world"

	// cloudflareRepo is Apache-2.0; pinned by commit.
	cloudflareRepo = "cloudflare/skills"
	cloudflareRef  = "b052c32bab7dd493513260228a36c88294f343f1"
)

var (
	gsxSkills        = []string{"gsx", "templ-to-gsx-migration"}
	cloudflareSkills = []string{"wrangler", "durable-objects", "workers-best-practices", "cloudflare"}
)

// skillFiles is a skill set: path relative to the skills directory -> contents.
type skillFiles map[string][]byte

// syncSkills writes the pinned skills into .claude/skills and reports sessions
// that must restart to see them.
func syncSkills(out io.Writer) error {
	want, err := pinnedSkills()
	if err != nil {
		return err
	}
	existed := dirExists(skillsDir)
	have, err := readDir(skillsDir)
	if err != nil {
		return err
	}
	if err := writeDir(skillsDir, want); err != nil {
		return err
	}

	fmt.Fprintf(out, "skills in %s:\n", skillsDir)
	fmt.Fprint(out, indent(string(want[lockFile])))
	if !existed {
		fmt.Fprintf(out, "\nThese are new: Claude Code reads %s at startup.\n", skillsDir)
	}
	if !sameFiles(have, want) {
		warnStaleSessions(out, time.Now())
	}
	return nil
}

// checkSkills fails when the skills on disk differ from the pins.
func checkSkills(out io.Writer) error {
	want, err := pinnedSkills()
	if err != nil {
		return err
	}
	have, err := readDir(skillsDir)
	if err != nil {
		return err
	}
	if diff := diffFiles(have, want); len(diff) > 0 {
		return fmt.Errorf("%s does not match its pins:\n%sfix with: mise run skills:sync", skillsDir, indent(strings.Join(diff, "\n")+"\n"))
	}
	warnStaleSessions(out, time.Now())
	return nil
}

// pinnedSkills collects every skill at its pinned version.
func pinnedSkills() (skillFiles, error) {
	files := skillFiles{}
	var lock []string

	gsxVersion, gsxDir, err := gsxModuleInfo()
	if err != nil {
		return nil, err
	}
	for _, name := range gsxSkills {
		if err := copyLocal(files, filepath.Join(gsxDir, "skills", name), name); err != nil {
			return nil, err
		}
		lock = append(lock, fmt.Sprintf("%s\t%s@%s", name, gsxModule, gsxVersion))
	}

	archive, err := download(fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", cloudflareRepo, cloudflareRef))
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("skills-%s/skills/", cloudflareRef)
	for _, name := range cloudflareSkills {
		if err := copyTar(files, archive, prefix+name+"/", name); err != nil {
			return nil, err
		}
		lock = append(lock, fmt.Sprintf("%s\tgithub.com/%s@%s", name, cloudflareRepo, cloudflareRef[:12]))
	}

	sort.Strings(lock)
	files[lockFile] = []byte(strings.Join(lock, "\n") + "\n")
	return files, nil
}

// gsxModuleInfo returns the pinned gsx version and its directory in the module
// cache, downloading it when needed.
func gsxModuleInfo() (version, dir string, err error) {
	if err := run(gsxModuleDir, "go", "mod", "download", gsxModule); err != nil {
		return "", "", err
	}
	version, err = output(gsxModuleDir, "go", "list", "-m", "-f", "{{.Version}}", gsxModule)
	if err != nil {
		return "", "", err
	}
	dir, err = output(gsxModuleDir, "go", "list", "-m", "-f", "{{.Dir}}", gsxModule)
	return version, dir, err
}

func copyLocal(files skillFiles, src, name string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[path.Join(name, filepath.ToSlash(rel))] = data
		return nil
	})
}

func copyTar(files skillFiles, archive []byte, prefix, name string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	found := false
	r := tar.NewReader(gz)
	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || !strings.HasPrefix(header.Name, prefix) {
			continue
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		files[path.Join(name, strings.TrimPrefix(header.Name, prefix))] = data
		found = true
	}
	if !found {
		return fmt.Errorf("skill %q not found in %s@%s", name, cloudflareRepo, cloudflareRef[:12])
	}
	return nil
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// readDir reads a skills directory; a missing directory is an empty set.
func readDir(dir string) (skillFiles, error) {
	files := skillFiles{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func writeDir(dir string, files skillFiles) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for name, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// diffFiles describes how have differs from want, most useful first.
func diffFiles(have, want skillFiles) []string {
	var diff []string
	for name, wantData := range want {
		haveData, ok := have[name]
		switch {
		case !ok:
			diff = append(diff, "missing: "+name)
		case !bytes.Equal(haveData, wantData):
			diff = append(diff, "changed: "+name)
		}
	}
	for name := range have {
		if _, ok := want[name]; !ok {
			diff = append(diff, "unexpected: "+name)
		}
	}
	sort.Strings(diff)
	return diff
}

func sameFiles(a, b skillFiles) bool { return len(diffFiles(a, b)) == 0 }

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
