// Command dev holds this repo's developer tooling. It is not part of the proxy
// binary: mise tasks build and run it, so nothing here ships to users.
//
// Each tool is a package of its own, so what one owns is never entangled with
// another, and this file does nothing but parse arguments and say what is
// available.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/rel"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sessionpin"
)

const usage = `dev: developer tooling for this repo (run through mise).

  dev browser <server> <probe> [path]   serve an app, drive it in a headless Chrome, run the probe
  dev session sync|check|verify|bump    pin Claude Code skills to session.toml
  dev release snapshot|packslip|publish publish a GitHub Release fully locally
`

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) >= 3 && args[0] == "browser":
		path := "/"
		if len(args) > 3 {
			path = args[3]
		}
		err = browser.Check(os.Stdout, args[1], args[2], path)
	case len(args) >= 1 && args[0] == "session":
		err = sessionpin.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *sessionpin.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
	case len(args) >= 1 && args[0] == "release":
		err = rel.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *rel.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
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
