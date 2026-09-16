package stage

import (
	"errors"
	"runtime"
	"testing"
)

// findChrome has to work on a machine that has Chrome in /Applications, one
// that has chromium on PATH, and one that has neither.
func TestFindChrome(t *testing.T) {
	onPath := func(name string) (string, error) {
		if name == "chromium" {
			return "/usr/local/bin/chromium", nil
		}
		return "", errors.New("not found")
	}
	none := func(string) (string, error) { return "", errors.New("not found") }
	never := func(string) bool { return false }
	only := func(want string) func(string) bool {
		return func(path string) bool { return path == want }
	}
	env := func(value string) func(string) string {
		return func(key string) string {
			if key == "CHROME" {
				return value
			}
			return ""
		}
	}
	installed := chromePaths[runtime.GOOS][0]

	for name, tc := range map[string]struct {
		getenv   func(string) string
		lookPath func(string) (string, error)
		exists   func(string) bool
		want     string
		wantErr  error
	}{
		"CHROME wins":       {env("/opt/my-chrome"), none, only("/opt/my-chrome"), "/opt/my-chrome", nil},
		"CHROME is missing": {env("/opt/gone"), onPath, never, "", nil},
		"installed":         {env(""), none, only(installed), installed, nil},
		"on PATH":           {env(""), onPath, never, "/usr/local/bin/chromium", nil},
		"nowhere":           {env(""), none, never, "", errNoChrome},
	} {
		got, err := findChrome(tc.getenv, tc.lookPath, tc.exists)
		switch {
		case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
			t.Errorf("%s: err = %v, want %v", name, err, tc.wantErr)
		case tc.want != "" && got != tc.want:
			t.Errorf("%s: findChrome() = %q, want %q", name, got, tc.want)
		case tc.want == "" && tc.wantErr == nil && err == nil:
			t.Errorf("%s: findChrome() = %q, want an error naming the problem", name, got)
		}
	}
}
