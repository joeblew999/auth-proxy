// Package session keeps the repo's Claude Code skills pinned. It writes them
// from their upstreams into .claude/skills, checks that the copy on disk still
// matches the pins, and proves that a fresh session can actually load them.
//
// What is pinned lives in session.toml; this package only reads it.
package session

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
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
	pinsFile  = "session.toml"
)

// skillFiles is a skill set: path relative to the skills directory -> contents.
type skillFiles map[string][]byte

// Sync writes the pinned skills into .claude/skills and reports sessions that
// must restart to see them. Files it does not own are left alone, so skills
// this repo writes itself survive a sync.
func Sync(out io.Writer) error {
	want, err := pinnedSkills()
	if err != nil {
		return err
	}
	existed := dirExists(skillsDir)
	have, err := readDir(skillsDir)
	if err != nil {
		return err
	}
	if err := writeOwned(skillsDir, have, want); err != nil {
		return err
	}

	fmt.Fprintf(out, "skills in %s:\n", skillsDir)
	fmt.Fprint(out, indent(string(want[lockFile])))
	if !existed {
		fmt.Fprintf(out, "\nThese are new: Claude Code reads %s at startup.\n", skillsDir)
	}
	p, err := loadPins()
	if err != nil {
		return err
	}
	if err := syncSettings(out, p.Claude); err != nil {
		return err
	}
	if !sameFiles(have, want) {
		warnStaleSessions(out, time.Now())
	}
	return nil
}

// Check fails when the skills on disk differ from the lock, or when a go.mod
// disagrees with the mise pin that is the source of truth for its version. It
// needs no network: file contents are compared against the hashes sync recorded
// in the lock.
func Check(out io.Writer) error {
	have, err := readDir(skillsDir)
	if err != nil {
		return err
	}
	want, err := lockedFiles()
	if err != nil {
		return err
	}
	if diff := diffLocked(have, want); len(diff) > 0 {
		return fmt.Errorf("%s does not match its pins:\n%sfix with: "+syncCmd, skillsDir, indent(strings.Join(diff, "\n")+"\n"))
	}
	p, err := loadPins()
	if err != nil {
		return err
	}
	if err := checkSettings(p.Claude); err != nil {
		return err
	}
	if err := checkPortablePaths(); err != nil {
		return err
	}
	warnStaleSessions(out, time.Now())
	return nil
}

// pinnedSkills collects every skill at its pinned version. The lock records
// one line per skill (its source) plus one line per file (its hash), so check
// can verify the files without downloading anything.
func pinnedSkills() (skillFiles, error) {
	p, err := loadPins()
	if err != nil {
		return nil, err
	}
	files := skillFiles{}
	var lock []string

	for _, source := range p.names() {
		s := p.Source[source]
		archive, err := download(fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", s.Repo, s.Ref))
		if err != nil {
			return nil, err
		}
		prefix := fmt.Sprintf("skills-%s/skills/", s.Ref)
		for _, name := range s.Skills {
			if err := copyTar(files, archive, prefix+name+"/", name, s.Repo, s.Ref); err != nil {
				return nil, err
			}
			lock = append(lock, lockSkill(name, fmt.Sprintf("github.com/%s@%s", s.Repo, s.Ref[:12]), files))
		}
	}

	sort.Strings(lock)
	files[lockFile] = []byte(strings.Join(lock, "\n") + "\n")
	return files, nil
}

// writeOwned writes want into dir, deleting only files sync owned before:
// anything in have that is neither in want nor owned by the previous sync is
// left alone. Owned paths come from the previous lock: every top-level skill
// directory it names, plus the lock file itself.
func writeOwned(dir string, have, want skillFiles) error {
	prefixes := ownedPaths(have[lockFile])
	for name := range have {
		if _, ok := want[name]; ok {
			continue
		}
		if !isOwned(name, prefixes) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	for name, data := range want {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	removeEmptyDirs(dir)
	return nil
}

// ownedPaths returns the top-level paths the previous sync owned: the lock
// file and every skill directory it names.
func ownedPaths(lock []byte) []string {
	owned := []string{lockFile}
	for _, line := range strings.Split(strings.TrimSpace(string(lock)), "\n") {
		if name, _, ok := strings.Cut(line, "\t"); ok && name != "" {
			owned = append(owned, name)
		}
	}
	return owned
}

func isOwned(name string, owned []string) bool {
	for _, prefix := range owned {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}

// removeEmptyDirs deletes directories left empty after owned files moved out,
// so a dropped skill leaves no empty folder behind.
func removeEmptyDirs(dir string) {
	var dirs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d)
	}
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
