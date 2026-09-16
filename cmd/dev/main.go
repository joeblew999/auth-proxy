// Command dev holds this repo's developer tooling. It is not part of the proxy
// binary: mise tasks build and run it, so nothing here ships to users.
package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `dev: developer tooling for this repo (run through mise).

  dev skills sync                       put the pinned gsx and Cloudflare skills into .claude/skills
  dev skills check                      fail when .claude/skills differs from the pins
  dev skills verify                     ask a fresh Claude Code session which skills it can see
  dev browser <server> <probe> [path]   serve an app, drive it in a headless Chrome, run the probe
`

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 2 && args[0] == "skills":
		switch args[1] {
		case "sync":
			err = syncSkills(os.Stdout)
		case "check":
			err = checkSkills(os.Stdout)
		case "verify":
			err = verifySkills(os.Stdout)
		default:
			fail(args)
		}
	case len(args) >= 3 && args[0] == "browser":
		path := "/"
		if len(args) > 3 {
			path = args[3]
		}
		err = browserCheck(os.Stdout, args[1], args[2], path)
	default:
		fail(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func fail(args []string) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", strings.Join(args, " "))
	}
	fmt.Fprint(os.Stderr, usage)
	os.Exit(2)
}
