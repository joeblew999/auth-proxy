package session

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
	"strings"
)

func copyTar(files skillFiles, archive []byte, prefix, name, repo, ref string) error {
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
		return fmt.Errorf("skill %q not found in %s@%s", name, repo, ref[:12])
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
// Symlinks mise made (packslip skills) and mise's own state file are skipped:
// mise owns those, not sync.
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
			if p != dir {
				if isSymlink(p) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if isSymlink(p) {
			return nil
		}
		// Not sync's files: mise's own state, and the record of what a real
		// session is allowed, which only verify writes.
		if base := filepath.Base(p); base == ".mise-skills.json" || base == sessionLockFile {
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

// isSymlink reports whether p itself is a symlink, without following it.
func isSymlink(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode()&os.ModeSymlink != 0
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
