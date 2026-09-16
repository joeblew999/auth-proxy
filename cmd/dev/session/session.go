// Package session keeps a repo's Claude Code skills pinned, and keeps the
// rest of the repo's Claude Code session from being decided somewhere else.
//
// What is pinned lives in session.toml; this package only reads it. It runs as
// `dev session ...` through cmd/dev, so every developer workflow stays in one
// binary.
package session

import (
	"io"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
)

// syncCmd is how this repo spells "run sync", quoted back in every error that
// a sync would fix. session.toml sets it to the mise task; the default covers
// a repo that runs the dev binary directly.
var syncCmd = "mise run session:sync"

// Usage is what cmd/dev prints for this verb.
const Usage = `dev session sync             write .claude/skills and the .claude/settings.json keys session.toml implies
dev session check            fail when either has drifted from session.toml
dev session verify [--update]  hold a fresh Claude Code session against SESSION.lock; --update records it
dev session bump [source]    move a pin in session.toml to upstream HEAD
dev session mcp              every MCP server .mcp.json declares connects
`

// Run is `dev session sync|check|verify|bump|mcp`.
func Run(verb string, args []string, stdout, stderr io.Writer) error {
	applySyncCommand()
	if len(args) == 0 {
		return cli.Usagef("session: sync, check, verify, bump or mcp")
	}
	switch args[0] {
	case "sync":
		if err := requireNoArgs(args); err != nil {
			return err
		}
		return Sync(stdout)
	case "check":
		if err := requireNoArgs(args); err != nil {
			return err
		}
		return Check(stdout)
	case "verify":
		update, ok := updateFlag(args[1:])
		if !ok {
			return cli.Usagef("session verify takes only --update")
		}
		return Verify(stdout, update)
	case "bump":
		return Bump(stdout, args[1:])
	case "mcp":
		if err := requireNoArgs(args); err != nil {
			return err
		}
		return MCP(stdout, stderr)
	}
	return cli.Usagef("session: unknown subcommand %q", args[0])
}

// applySyncCommand takes sync_command from session.toml before anything runs,
// so that commands which never load the pins -- verify reads only the lock --
// still name this repo's own way of running a sync. A broken or missing file
// is not this function's business; whatever runs next reports it properly.
func applySyncCommand() {
	var config struct {
		SyncCommand string `toml:"sync_command"`
	}
	if _, err := toml.DecodeFile(pinsFile, &config); err == nil && config.SyncCommand != "" {
		syncCmd = config.SyncCommand
	}
}

func requireNoArgs(args []string) error {
	if len(args) != 1 {
		return cli.Usagef("session %s takes no arguments", args[0])
	}
	return nil
}

// updateFlag reads verify's only flag. A mise task passes
// `--update=$usage_update`, which is `--update=` when nobody gave the flag.
func updateFlag(args []string) (update, ok bool) {
	if len(args) == 0 {
		return false, true
	}
	if len(args) != 1 {
		return false, false
	}
	name, value, _ := strings.Cut(args[0], "=")
	if name != "--update" {
		return false, false
	}
	var b cli.Bool
	if err := b.Set(value); err != nil {
		return false, false
	}
	return bool(b), true
}
