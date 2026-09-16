package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// pins is everything session.toml says: named sources, each either a Go module
// in the local module cache or a GitHub repo at a pinned commit.
type pins struct {
	Source map[string]sourcePins `toml:"source"`
	Claude claudePins            `toml:"claude"`
	// SyncCommand is how this repo prefers sync to be run, quoted back in
	// every error a sync would fix. A repo whose front door is a task runner
	// sets it to that task; left empty, errors name this binary.
	SyncCommand string `toml:"sync_command"`
}

// claudePins is the part of the Claude Code session this repo owns: which
// marketplace plugins must not load, and where MCP servers come from. Sync
// writes it into .claude/settings.json; check fails when the two disagree.
type claudePins struct {
	BlockedPlugins []string `toml:"blocked_plugins"`
	// ClaudeAIConnectors is a pointer so that leaving it out means "not this
	// repo's business" rather than "off": a repo adopting session must not
	// silently lose its connectors by not mentioning them.
	ClaudeAIConnectors *bool `toml:"claude_ai_connectors"`
	ApproveMCPServers  bool  `toml:"approve_mcp_servers"`
}

type sourcePins struct {
	Kind      string   `toml:"kind"`
	Module    string   `toml:"module"`
	ModuleDir string   `toml:"module_dir"`
	Repo      string   `toml:"repo"`
	Ref       string   `toml:"ref"`
	Skills    []string `toml:"skills"`
}

// kind resolves the source kind, defaulting from whichever pin is set.
func (s sourcePins) kind() string {
	if s.Kind != "" {
		return s.Kind
	}
	if s.Module != "" {
		return "gomod"
	}
	return "github"
}

// loadPins reads session.toml. Every error names the fix: the file is the only
// place a skill or a pin is named. Unknown keys fail: a typo must not silently
// drop a source.
func loadPins() (pins, error) {
	var p pins
	meta, err := toml.DecodeFile(pinsFile, &p)
	if err != nil {
		return pins{}, fmt.Errorf("%s: %w; fix the file, then: "+syncCmd, pinsFile, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return pins{}, fmt.Errorf("%s: unknown key %q; fix the file, then: "+syncCmd, pinsFile, undecoded[0])
	}
	if len(p.Source) == 0 {
		return pins{}, fmt.Errorf("%s lists no skills", pinsFile)
	}
	names := p.names()
	for _, name := range names {
		s := p.Source[name]
		switch s.kind() {
		case "gomod":
			if s.Module == "" || s.ModuleDir == "" {
				return pins{}, fmt.Errorf("%s: [source.%s] needs module and module_dir; fix the file, then: "+syncCmd, pinsFile, name)
			}
		case "github":
			if s.Repo == "" || s.Ref == "" {
				return pins{}, fmt.Errorf("%s: [source.%s] needs repo and ref; fix the file, then: "+syncCmd, pinsFile, name)
			}
		default:
			return pins{}, fmt.Errorf("%s: [source.%s] unknown kind %q; want gomod or github", pinsFile, name, s.Kind)
		}
		if len(s.Skills) == 0 {
			return pins{}, fmt.Errorf("%s: [source.%s] lists no skills", pinsFile, name)
		}
	}
	if p.SyncCommand != "" {
		syncCmd = p.SyncCommand
	}
	return p, nil
}

// names returns source names in sorted order, so sync and check are stable.
func (p pins) names() []string {
	var names []string
	for name := range p.Source {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkToolPins fails when a gomod source's go.mod disagrees with its mise pin.
// mise is the source of truth for the version; the module follows. The module
// comes from session.toml; the mise key is whichever go: tool extends it.
func checkToolPins(out io.Writer) error {
	p, err := loadPins()
	if err != nil {
		return err
	}
	misePins, err := miseToolPins()
	if err != nil {
		return err
	}
	if misePins == nil {
		// No mise.toml: nothing claims to be the source of truth for a
		// module version, so there is no disagreement to find.
		return nil
	}
	for _, name := range p.names() {
		s := p.Source[name]
		if s.kind() != "gomod" {
			continue
		}
		var keys []string
		for key := range misePins {
			if key == "go:"+s.Module || strings.HasPrefix(key, "go:"+s.Module+"/") {
				keys = append(keys, key)
			}
		}
		switch len(keys) {
		case 0:
			return fmt.Errorf("mise pins no go: tool under %q; add it to [tools] in mise.toml", s.Module)
		case 1:
			// The one tool that carries the module's version.
		default:
			sort.Strings(keys)
			return fmt.Errorf("mise pins several go: tools under %q:\n%s\nname the version carrier in %s",
				s.Module, indent(strings.Join(keys, "\n")), pinsFile)
		}
		want := misePins[keys[0]]
		got, err := output(s.ModuleDir, "go", "list", "-m", "-f", "{{.Version}}", s.Module)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s/go.mod has %s %s, but mise has %s (%s)\nfix with: go-mod-upgrade in %s, or change the mise pin",
				s.ModuleDir, s.Module, got, want, keys[0], s.ModuleDir)
		}
		fmt.Fprintf(out, "%s %s matches mise\n", s.Module, want)
	}
	return nil
}

// miseToolPins asks mise which go: tools are active and at what version.
//
// It asks rather than reading mise.toml, because mise.toml is not the whole
// answer: mise merges a config hierarchy, and a pin of "latest" resolves to a
// real version only mise knows. Parsing another tool's config behind its back
// gets both wrong.
//
// A machine without mise gets a nil map and no error: nothing there claims to
// be the source of truth for a tool version, so there is no disagreement to
// find.
func miseToolPins() (map[string]string, error) {
	if _, err := exec.LookPath("mise"); err != nil {
		return nil, nil
	}
	out, err := output(".", "mise", "ls", "--current", "--json")
	if err != nil {
		return nil, fmt.Errorf("mise ls: %w", err)
	}
	var listed map[string][]struct {
		Version          string `json:"version"`
		RequestedVersion string `json:"requested_version"`
		Active           bool   `json:"active"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		return nil, fmt.Errorf("mise ls --json: %w", err)
	}
	pins := map[string]string{}
	for tool, installs := range listed {
		if !strings.HasPrefix(tool, "go:") {
			continue
		}
		for _, install := range installs {
			if !install.Active {
				continue
			}
			// The resolved version is what a go.mod can be compared against;
			// "latest" cannot.
			if install.Version != "" {
				pins[tool] = install.Version
			} else {
				pins[tool] = install.RequestedVersion
			}
		}
	}
	return pins, nil
}
