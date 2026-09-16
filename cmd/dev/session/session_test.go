package session

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	content := "[source.a]\nmodule = \"example.com/mod\"\nmodule_dir = \"spike\"\nskills = [\"x\", \"y\"]\n\n[source.b]\nrepo = \"org/repo\"\nref = \"abc123\"\nskills = [\"z\"]\n"
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
	if a.kind() != "gomod" || a.Module != "example.com/mod" || a.ModuleDir != "spike" || len(a.Skills) != 2 {
		t.Errorf("source a = %+v", a)
	}
	b := p.Source["b"]
	if b.kind() != "github" || b.Repo != "org/repo" || b.Ref != "abc123" || len(b.Skills) != 1 {
		t.Errorf("source b = %+v", b)
	}
}

func TestLoadPinsRejectsUnknownKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[source.a]\nmodule = \"x\"\nmodule_dir = \"y\"\nskills = [\"z\"]\nbogus = 1\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPins(); err == nil {
		t.Error("loadPins succeeded with an unknown key, want an error naming the fix")
	}
}

func TestLoadPinsRejectsBadSource(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[source.a]\nskills = [\"z\"]\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPins(); err == nil {
		t.Error("loadPins succeeded with no module or repo, want an error naming the fix")
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

// A repo with no mise.toml still has to be able to run check.
func TestCheckToolPinsSkipsWithoutMise(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeFile("session.toml", "[source.a]\nrepo = \"o/r\"\nref = \"abc\"\nskills = [\"z\"]\n"); err != nil {
		t.Fatal(err)
	}
	if err := checkToolPins(io.Discard); err != nil {
		t.Errorf("checkToolPins = %v; want it skipped when nothing pins tool versions", err)
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

// mise, not mise.toml, is the source of truth for a tool version: it merges a
// config hierarchy and resolves "latest" to something a go.mod can be compared
// against. Reading the file directly saw neither.
func TestMiseToolPinsResolvesVersions(t *testing.T) {
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise not installed")
	}
	pins, err := miseToolPins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) == 0 {
		t.Skip("no go: tools active here")
	}
	for tool, version := range pins {
		if !strings.HasPrefix(tool, "go:") {
			t.Errorf("miseToolPins returned a non-go tool: %q", tool)
		}
		if version == "latest" || version == "" {
			t.Errorf("%s = %q; want the version mise resolved, not the request", tool, version)
		}
	}
}

// The gomod source kind has no user in this repo since gsx and gsxui moved to
// packslip, so nothing exercised it end to end. These two do.
func TestCopyLocalKeepsTheTreeUnderTheSkillName(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(src, "SKILL.md"), "top"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(src, "references", "a.md"), "nested"); err != nil {
		t.Fatal(err)
	}
	files := skillFiles{}
	if err := copyLocal(files, src, "demo"); err != nil {
		t.Fatal(err)
	}
	if string(files["demo/SKILL.md"]) != "top" {
		t.Errorf("demo/SKILL.md = %q", files["demo/SKILL.md"])
	}
	if string(files["demo/references/a.md"]) != "nested" {
		t.Errorf("demo/references/a.md = %q; nested files must keep their path", files["demo/references/a.md"])
	}
	if len(files) != 2 {
		t.Errorf("copyLocal wrote %d files, want 2: %v", len(files), files)
	}
}

func TestGomodInfoResolvesAModuleInTheCache(t *testing.T) {
	// A real dependency of this module, so the local cache has it.
	version, dir, err := gomodInfo(".", "github.com/BurntSushi/toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "v") {
		t.Errorf("version = %q; want the version go resolved", version)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("dir = %q; want a directory in the module cache (%v)", dir, err)
	}
}

// SESSION.lock is an allowlist: anything the session gained that it does not
// name fails, whether it shadows a pinned skill or not.
func TestDiffSessionCatchesArrivalsAndLosses(t *testing.T) {
	gone, arrived := diffSession(
		[]string{"cloudflare", "gsx", "run"},
		[]string{"cloudflare", "run", "cloudflare:sandbox-sdk", "some-synced-skill"},
	)
	if len(gone) != 1 || gone[0] != "gsx" {
		t.Errorf("gone = %v; want the skill that stopped loading", gone)
	}
	if len(arrived) != 2 {
		t.Errorf("arrived = %v; want both the plugin skill and the unnamespaced one", arrived)
	}
}

func TestArrivalAdviceNamesThePlugin(t *testing.T) {
	got := arrivalAdvice([]string{"cloudflare:sandbox-sdk", "cloudflare:web-perf"})
	if !strings.Contains(got, "cloudflare") || !strings.Contains(got, "blocked_plugins") {
		t.Errorf("advice = %q; want it to name the plugin and the fix", got)
	}
	// A skill with no namespace cannot be blocked by any setting; the advice
	// must say so instead of suggesting one.
	got = arrivalAdvice([]string{"some-synced-skill"})
	if strings.Contains(got, "blocked_plugins") {
		t.Errorf("advice = %q; a non-plugin skill cannot be blocked", got)
	}
	if !strings.Contains(got, "--update") {
		t.Errorf("advice = %q; want the re-bless command", got)
	}
}
