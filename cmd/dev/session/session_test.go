package session

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func writeFile(name, content string) error {
	return os.WriteFile(name, []byte(content), 0o644)
}

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

func TestLoadPins(t *testing.T) {
	t.Chdir(t.TempDir())
	content := "[source.a]\nrepo = \"org/a\"\nref = \"def456\"\nskills = [\"x\", \"y\"]\n\n[source.b]\nrepo = \"org/repo\"\nref = \"abc123\"\nskills = [\"z\"]\n"
	if err := writeFile("session.toml", content); err != nil {
		t.Fatal(err)
	}
	p, err := loadPins()
	if err != nil {
		t.Fatal(err)
	}
	if got := p.names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("names = %v", got)
	}
	a := p.Source["a"]
	if a.Repo != "org/a" || a.Ref != "def456" || len(a.Skills) != 2 {
		t.Errorf("source a = %+v", a)
	}
	b := p.Source["b"]
	if b.Repo != "org/repo" || b.Ref != "abc123" || len(b.Skills) != 1 {
		t.Errorf("source b = %+v", b)
	}
}

func TestLoadPinsRejectsUnknownKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[source.a]\nrepo = \"x/y\"\nref = \"abc\"\nskills = [\"z\"]\nbogus = 1\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPins(); err == nil {
		t.Error("loadPins succeeded with an unknown key, want an error naming the fix")
	}
}

func TestLoadPinsAllowsOnlyClaudeSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[claude]\nblocked_plugins = [\"x@y\"]\n"); err != nil {
		t.Fatal(err)
	}
	p, err := loadPins()
	if err != nil {
		t.Fatalf("a repo pinning only its Claude settings was rejected: %v", err)
	}
	if len(p.Source) != 0 || len(p.Claude.BlockedPlugins) != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestLoadPinsRejectsBadSource(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[source.a]\nskills = [\"z\"]\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPins(); err == nil {
		t.Error("loadPins succeeded with no repo or ref, want an error naming the fix")
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
func TestSyncSettingsKeepsWhatItDoesNotOwn(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(settingsFile, `{"hooks":{"Stop":[]},"enabledPlugins":{"stale@old":false}}`); err != nil {
		t.Fatal(err)
	}
	c := claudePins{BlockedPlugins: []string{"a@b"}, ApproveMCPServers: true}
	if err := syncSettings(io.Discard, c); err != nil {
		t.Fatal(err)
	}
	have, err := readSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := have["hooks"]; !ok {
		t.Error("sync dropped hooks, which it does not own")
	}
	if err := checkSettings(c); err != nil {
		t.Errorf("check failed right after sync: %v", err)
	}
	blocked, _ := have["enabledPlugins"].(map[string]any)
	if blocked["a@b"] != false || len(blocked) != 1 {
		t.Errorf("enabledPlugins = %v; want exactly the blocked list", blocked)
	}
}

func TestCheckSettingsCatchesHandEdits(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(settingsFile, `{"enabledPlugins":{"a@b":true}}`); err != nil {
		t.Fatal(err)
	}
	err := checkSettings(claudePins{BlockedPlugins: []string{"a@b"}})
	if err == nil || !strings.Contains(err.Error(), syncCmd) {
		t.Errorf("checkSettings = %v; want an error naming %q as the fix", err, syncCmd)
	}
}

// Another repo runs this through its own task runner, so the fix an error
// names has to come from that repo, not from this one's habits.
func TestSyncCommandComesFromPins(t *testing.T) {
	t.Chdir(t.TempDir())
	defer func(old string) { syncCmd = old }(syncCmd)
	syncCmd = "dev session sync"
	if err := writeFile("session.toml", "sync_command = \"just skills\"\n[source.a]\nrepo = \"o/r\"\nref = \"abc\"\nskills = [\"z\"]\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPins(); err != nil {
		t.Fatal(err)
	}
	if syncCmd != "just skills" {
		t.Errorf("syncCmd = %q; want the one session.toml names", syncCmd)
	}
}

// A repo that never mentions connectors must not have them turned off behind
// its back, so an absent key writes no setting at all.
func TestConnectorsUnmanagedWhenUnset(t *testing.T) {
	if _, ok := wantSettings(claudePins{})["disableClaudeAiConnectors"]; ok {
		t.Error("an unset claude_ai_connectors disabled them anyway")
	}
	off := false
	if want := wantSettings(claudePins{ClaudeAIConnectors: &off}); want["disableClaudeAiConnectors"] != true {
		t.Errorf("claude_ai_connectors = false did not disable them: %v", want)
	}
	on := true
	if _, ok := wantSettings(claudePins{ClaudeAIConnectors: &on})["disableClaudeAiConnectors"]; ok {
		t.Error("claude_ai_connectors = true wrote a setting; it cannot force them on")
	}
}
func TestCheckPortablePathsCatchesAbsoluteCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(".mcp.json", `{"mcpServers":{"hk":{"command":"/opt/homebrew/bin/mise"}}}`); err != nil {
		t.Fatal(err)
	}
	err := checkPortablePaths()
	if err == nil || !strings.Contains(err.Error(), "/opt/homebrew/bin/mise") {
		t.Errorf("checkPortablePaths = %v; want it to name the offending command", err)
	}
	// Nested inside a hooks array, which is where the other one hid.
	if err := writeFile(settingsFile, `{"hooks":{"Stop":[{"hooks":[{"command":"/usr/local/bin/x"}]}]}}`); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(".mcp.json", `{"mcpServers":{"hk":{"command":"mise"}}}`); err != nil {
		t.Fatal(err)
	}
	if err := checkPortablePaths(); err == nil || !strings.Contains(err.Error(), "/usr/local/bin/x") {
		t.Errorf("checkPortablePaths missed a command nested in hooks: %v", err)
	}
	if err := writeFile(settingsFile, `{"hooks":{"Stop":[{"hooks":[{"command":"mise exec -- hk"}]}]}}`); err != nil {
		t.Fatal(err)
	}
	if err := checkPortablePaths(); err != nil {
		t.Errorf("checkPortablePaths = %v; want bare commands to pass", err)
	}
}

func TestSessionLockRecordsWhichClaudeCodeWroteIt(t *testing.T) {
	names, by := parseSessionLock(formatSessionLock([]string{"gsx", "wrangler"}, "2.1.272"))
	if by != "2.1.272" || strings.Join(names, ",") != "gsx,wrangler" {
		t.Fatalf("got %v by %q", names, by)
	}
	names, by = parseSessionLock("# an older lock\nwrangler\ngsx\n")
	if by != "" || strings.Join(names, ",") != "gsx,wrangler" {
		t.Fatalf("old format: got %v by %q", names, by)
	}
}
