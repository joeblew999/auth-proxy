// Command skillpin keeps a repo's Claude Code skills pinned, and keeps the rest
// of the repo's Claude Code session from being decided somewhere else.
//
// A repo needs two things to adopt it: a skills.toml naming what it pins, and a
// way to run this command. Nothing else here is specific to any one repo -- no
// mise, no task names, no layout. Run it from the repo root:
//
//	skillpin sync     write the pinned skills, and the settings skills.toml implies
//	skillpin check    fail when either has drifted from skills.toml
//	skillpin verify   ask a fresh Claude Code session what it can actually see
//	skillpin bump     move github pins to upstream HEAD
//
// The point is that one file decides. Marketplace plugins and claude.ai
// connectors can put skills in a session that no repo file mentions, at
// versions nobody here chose; sync writes the settings that keep them out, and
// verify catches whatever still gets through.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// syncCmd is how this repo spells "run sync", quoted back in every error that
// a sync would fix. It defaults to the binary's own name, and skills.toml can
// set sync_command when a task runner is the front door instead.
var syncCmd = "skillpin sync"

const usage = `skillpin: pin a repo's Claude Code skills, plugins and MCP servers to skills.toml.

  skillpin sync     write .claude/skills and the .claude/settings.json keys skills.toml implies
  skillpin check    fail when either has drifted from skills.toml (run it in CI)
  skillpin verify   ask a fresh Claude Code session which skills it can see
  skillpin bump [source]
                    move github pins in skills.toml to upstream HEAD

Run from the repo root. See skills.toml for what is pinned.
`

func main() {
	syncCmd = defaultSyncCmd()
	args := os.Args[1:]
	if len(args) == 0 {
		fail(args)
	}
	var err error
	switch args[0] {
	case "sync":
		requireNoArgs(args)
		err = Sync(os.Stdout)
	case "check":
		requireNoArgs(args)
		err = Check(os.Stdout)
	case "verify":
		requireNoArgs(args)
		err = Verify(os.Stdout)
	case "bump":
		err = Bump(os.Stdout, args[1:])
	default:
		fail(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// defaultSyncCmd names this binary as the user invoked it, so the fix an error
// suggests is one they can paste. SKILLPIN_SYNC_CMD wins, for a repo that runs
// it through something else and has no skills.toml to read yet.
func defaultSyncCmd() string {
	if cmd := os.Getenv("SKILLPIN_SYNC_CMD"); cmd != "" {
		return cmd
	}
	name := filepath.Base(os.Args[0])
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "skillpin"
	}
	return name + " sync"
}

func requireNoArgs(args []string) {
	if len(args) != 1 {
		fail(args)
	}
}

func fail(args []string) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", strings.Join(args, " "))
	}
	fmt.Fprint(os.Stderr, usage)
	os.Exit(2)
}
