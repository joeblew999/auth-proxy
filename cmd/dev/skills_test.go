package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ps prints start times in the machine's locale, which broke the shell version
// of this twice, so elapsed time is parsed here and tested.
func TestParseElapsed(t *testing.T) {
	cases := map[string]time.Duration{
		"05:09":       5*time.Minute + 9*time.Second,
		"17:32:20":    17*time.Hour + 32*time.Minute + 20*time.Second,
		"1-04:05:06":  24*time.Hour + 4*time.Hour + 5*time.Minute + 6*time.Second,
		"12-00:00:00": 12 * 24 * time.Hour,
		"  01:00  ":   time.Minute,
	}
	for etime, want := range cases {
		got, err := parseElapsed(etime)
		if err != nil || got != want {
			t.Errorf("parseElapsed(%q) = %v, %v; want %v", etime, got, err, want)
		}
	}
	for _, bad := range []string{"", "nonsense", "10", "a:b", "x-01:00"} {
		if _, err := parseElapsed(bad); err == nil {
			t.Errorf("parseElapsed(%q) succeeded, want an error naming the format", bad)
		}
	}
}

func TestIsClaudeBinary(t *testing.T) {
	yes := []string{
		"/Users/a/.vscode/extensions/anthropic.claude-code-2.1.271-darwin-arm64/resources/native-binary/claude",
		"/Users/a/.local/bin/claude",
	}
	no := []string{"claude", "/bin/zsh -c claude", "/usr/bin/claude-helper", "/opt/claude/other"}
	for _, command := range yes {
		if !isClaudeBinary(command) {
			t.Errorf("isClaudeBinary(%q) = false, want true", command)
		}
	}
	for _, command := range no {
		if isClaudeBinary(command) {
			t.Errorf("isClaudeBinary(%q) = true, want false", command)
		}
	}
}

func TestDiffFiles(t *testing.T) {
	have := skillFiles{"gsx/SKILL.md": []byte("a"), "old/SKILL.md": []byte("x")}
	want := skillFiles{"gsx/SKILL.md": []byte("b"), "new/SKILL.md": []byte("c")}
	got := strings.Join(diffFiles(have, want), "; ")
	if got != "changed: gsx/SKILL.md; missing: new/SKILL.md; unexpected: old/SKILL.md" {
		t.Errorf("diffFiles() = %q", got)
	}
	if !sameFiles(want, want) {
		t.Error("sameFiles() = false for identical sets")
	}
}

func TestWarnStaleSessionsSaysNothingWithoutSkills(t *testing.T) {
	var out strings.Builder
	t.Chdir(t.TempDir())
	warnStaleSessions(&out, time.Now())
	if out.Len() != 0 {
		t.Errorf("warned without a skills directory: %q", out.String())
	}
}

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
