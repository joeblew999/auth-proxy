package session

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// pins is everything session.toml says: named sources, each a GitHub repo at
// a pinned commit, and the part of the Claude Code session this repo owns.
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

// sourcePins is one GitHub repo at one commit, and the skills taken from its
// skills/ directory. A commit, not a tag: the one upstream this repo needs
// (cloudflare/skills) ships no releases, and a tool that does ships a packslip
// release, which mise pins and links without any help from here.
type sourcePins struct {
	Repo   string   `toml:"repo"`
	Ref    string   `toml:"ref"`
	Skills []string `toml:"skills"`
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
	names := p.names()
	for _, name := range names {
		s := p.Source[name]
		if s.Repo == "" || s.Ref == "" {
			return pins{}, fmt.Errorf("%s: [source.%s] needs repo and ref; fix the file, then: "+syncCmd, pinsFile, name)
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
