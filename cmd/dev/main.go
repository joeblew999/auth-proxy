// Command dev holds this repo's developer tooling. It is not part of the proxy
// binary: mise tasks build and run it, so nothing here ships to users.
//
//	dev skills sync     put the pinned skills into .claude/skills
//	dev skills check    fail when .claude/skills differs from the pins
//	dev skills verify   prove a fresh Claude Code session loads them
package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `dev: developer tooling for this repo (run through mise).

  dev skills sync     put the pinned gsx and Cloudflare skills into .claude/skills
  dev skills check    fail when .claude/skills differs from the pins
  dev skills verify   ask a fresh Claude Code session which skills it can see
`

func main() {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "skills" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch args[1] {
	case "sync":
		err = syncSkills(os.Stdout)
	case "check":
		err = checkSkills(os.Stdout)
	case "verify":
		err = verifySkills(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", strings.Join(args, " "), usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
