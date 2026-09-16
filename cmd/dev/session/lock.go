package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// lockedFiles reads the file hashes sync recorded in the lock.
func lockedFiles() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(skillsDir, lockFile))
	if err != nil {
		return nil, fmt.Errorf("%w; run: "+syncCmd, err)
	}
	files := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		hash := strings.TrimSpace(rest)
		// A skill's source line ("gsx\tgithub.com/gsxhq/gsx@v0.1.1") is not
		// a hash: it has slashes and an @. A file line is 64 hex chars.
		if len(hash) != 64 {
			continue
		}
		files[name] = hash
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s lists no skills", lockFile)
	}
	return files, nil
}

// diffLocked compares files on disk against the lock hashes. The lock itself
// is skipped: sync rewrites it, so its hash can never match.
func diffLocked(have skillFiles, want map[string]string) []string {
	var diff []string
	for name, data := range have {
		if name == lockFile {
			continue
		}
		hash, ok := want[name]
		switch {
		case !ok:
			diff = append(diff, "unexpected: "+name)
		case hashFile(data) != hash:
			diff = append(diff, "changed: "+name)
		}
	}
	for name := range want {
		if _, ok := have[name]; !ok {
			diff = append(diff, "missing: "+name)
		}
	}
	sort.Strings(diff)
	return diff
}

func hashFile(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// lockSkill formats a skill's lock lines: its source, then one hash per file
// under its directory. prefix matches only this skill's own files, so an
// earlier skill's files never leak into a later skill's lines.
func lockSkill(name, source string, files skillFiles) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("%s\t%s", name, source))
	prefix := name + "/"
	var names []string
	for file := range files {
		if file == lockFile {
			continue
		}
		if file == name || strings.HasPrefix(file, prefix) {
			names = append(names, file)
		}
	}
	sort.Strings(names)
	for _, file := range names {
		lines = append(lines, fmt.Sprintf("%s\t%s", file, hashFile(files[file])))
	}
	return strings.Join(lines, "\n")
}

// sortedFiles returns every file path in files, sorted, skipping the lock.
func sortedFiles(files skillFiles) []string {
	var names []string
	for file := range files {
		if file == lockFile {
			continue
		}
		names = append(names, file)
	}
	sort.Strings(names)
	return names
}
