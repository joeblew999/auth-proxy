package skills

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// pins is everything skills.toml says: named sources, each either a Go module
// in the local module cache or a GitHub repo at a pinned commit.
type pins struct {
	Source map[string]sourcePins `toml:"source"`
	Claude claudePins            `toml:"claude"`
}

// claudePins is the part of the Claude Code session this repo owns: which
// marketplace plugins must not load, and where MCP servers come from. Sync
// writes it into .claude/settings.json; check fails when the two disagree.
type claudePins struct {
	BlockedPlugins     []string `toml:"blocked_plugins"`
	ClaudeAIConnectors bool     `toml:"claude_ai_connectors"`
	ApproveMCPServers  bool     `toml:"approve_mcp_servers"`
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

// loadPins reads skills.toml. Every error names the fix: the file is the only
// place a skill or a pin is named. Unknown keys fail: a typo must not silently
// drop a source.
func loadPins() (pins, error) {
	var p pins
	meta, err := toml.DecodeFile(pinsFile, &p)
	if err != nil {
		return pins{}, fmt.Errorf("%s: %w; fix the file, then: mise run dev:skills:sync", pinsFile, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return pins{}, fmt.Errorf("%s: unknown key %q; fix the file, then: mise run dev:skills:sync", pinsFile, undecoded[0])
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
				return pins{}, fmt.Errorf("%s: [source.%s] needs module and module_dir; fix the file, then: mise run dev:skills:sync", pinsFile, name)
			}
		case "github":
			if s.Repo == "" || s.Ref == "" {
				return pins{}, fmt.Errorf("%s: [source.%s] needs repo and ref; fix the file, then: mise run dev:skills:sync", pinsFile, name)
			}
		default:
			return pins{}, fmt.Errorf("%s: [source.%s] unknown kind %q; want gomod or github", pinsFile, name, s.Kind)
		}
		if len(s.Skills) == 0 {
			return pins{}, fmt.Errorf("%s: [source.%s] lists no skills", pinsFile, name)
		}
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
// comes from skills.toml; the mise key is whichever go: tool extends it.
func checkToolPins(out io.Writer) error {
	p, err := loadPins()
	if err != nil {
		return err
	}
	misePins, err := miseToolPins()
	if err != nil {
		return err
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
			return fmt.Errorf("mise.toml pins no go: tool under %q; add it under [tools]", s.Module)
		case 1:
			// The one tool that carries the module's version.
		default:
			sort.Strings(keys)
			return fmt.Errorf("mise.toml pins several go: tools under %q:\n%s\nname the version carrier in %s",
				s.Module, indent(strings.Join(keys, "\n")), pinsFile)
		}
		want := misePins[keys[0]]
		got, err := output(s.ModuleDir, "go", "list", "-m", "-f", "{{.Version}}", s.Module)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s/go.mod has %s %s, but mise.toml pins %s (%s)\nfix with: go-mod-upgrade in %s, or change the mise pin",
				s.ModuleDir, s.Module, got, want, keys[0], s.ModuleDir)
		}
		fmt.Fprintf(out, "%s %s matches mise\n", s.Module, want)
	}
	return nil
}

// miseToolPins reads the go: tool pins from mise.toml.
func miseToolPins() (map[string]string, error) {
	var config struct {
		Tools map[string]string `toml:"tools"`
	}
	if _, err := toml.DecodeFile("mise.toml", &config); err != nil {
		return nil, fmt.Errorf("mise.toml: %w", err)
	}
	pins := map[string]string{}
	for key, value := range config.Tools {
		if strings.HasPrefix(key, "go:") {
			pins[key] = value
		}
	}
	return pins, nil
}
