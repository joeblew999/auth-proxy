// Package sessionpin keeps a repo's Claude Code skills pinned, and keeps the
// rest of the repo's Claude Code session from being decided somewhere else.
//
// What is pinned lives in session.toml; this package only reads it. It runs as
// `dev session ...` through cmd/dev, so every developer workflow stays in one
// binary.
package sessionpin

import (
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
)

// syncCmd is how this repo spells "run sync", quoted back in every error that
// a sync would fix. session.toml sets it to the mise task; the default covers
// a repo that runs the dev binary directly.
var syncCmd = "mise run dev:session:sync"

const usage = `dev session: pin a repo's Claude Code skills, plugins and MCP servers to session.toml.

  dev session sync     write .claude/skills and the .claude/settings.json keys session.toml implies
  dev session check    fail when either has drifted from session.toml (run it in CI)
  dev session verify [--update]
                    hold a fresh Claude Code session against SESSION.lock; --update records it
  dev session bump [source]
                    move github pins in session.toml to upstream HEAD

Run from the repo root. See session.toml for what is pinned.
`

// Run dispatches the session subcommand. Args are everything after "session".
func Run(args []string, stdout, stderr io.Writer) error {
	applySyncCommand()
	if len(args) == 0 {
		fmt.Fprint(stderr, usageMessage(nil))
		return errUsage(nil)
	}
	switch args[0] {
	case "sync":
		if err := requireNoArgs(args); err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		return Sync(stdout)
	case "check":
		if err := requireNoArgs(args); err != nil {
			fmt.Fprint(stderr, usageMessage(args))
			return err
		}
		return Check(stdout)
	case "verify":
		update, ok := updateFlag(args[1:])
		if !ok {
			fmt.Fprint(stderr, usageMessage(args))
			return errUsage(args)
		}
		return Verify(stdout, update)
	case "bump":
		return Bump(stdout, args[1:])
	default:
		fmt.Fprint(stderr, usageMessage(args))
		return errUsage(args)
	}
}

// Usage writes the session help text.
func Usage(w io.Writer) { fmt.Fprint(w, usage) }

// UsageError means the arguments were wrong; the caller prints the usage.
type UsageError struct{ args []string }

func (e *UsageError) Error() string { return "usage" }

func errUsage(args []string) error { return &UsageError{args: args} }

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
		return errUsage(args)
	}
	return nil
}

func usageMessage(args []string) string {
	var b strings.Builder
	if len(args) > 0 {
		fmt.Fprintf(&b, "unknown command %q\n\n", strings.Join(args, " "))
	}
	b.WriteString(usage)
	return b.String()
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
