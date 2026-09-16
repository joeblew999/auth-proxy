// Command dev holds this repo's developer tooling. It is not part of the proxy
// binary: mise tasks build and run it, so nothing here ships to users.
//
// Each tool is a package of its own, so what one of them owns is never entangled
// with another, and this file does nothing but parse arguments and say what is
// available.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/skills"
)

const usage = `dev: developer tooling for this repo (run through mise).

  dev skills sync                       put the pinned skills from skills.toml into .claude/skills
  dev skills check                      fail when .claude/skills differs from the pins
  dev skills verify                     ask a fresh Claude Code session which skills it can see
  dev skills bump [source]              move github pins in skills.toml to upstream HEAD
  dev browser <server> <probe> [path]   serve an app, drive it in a headless Chrome, run the probe
`

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) >= 2 && args[0] == "skills":
		switch args[1] {
		case "sync":
			if len(args) != 2 {
				fail(args)
			}
			err = skills.Sync(os.Stdout)
		case "check":
			if len(args) != 2 {
				fail(args)
			}
			err = skills.Check(os.Stdout)
		case "verify":
			if len(args) != 2 {
				fail(args)
			}
			err = skills.Verify(os.Stdout)
		case "bump":
			err = skills.Bump(os.Stdout, args[2:])
		default:
			fail(args)
		}
	case len(args) >= 3 && args[0] == "browser":
		path := "/"
		if len(args) > 3 {
			path = args[3]
		}
		err = browser.Check(os.Stdout, args[1], args[2], path)
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
