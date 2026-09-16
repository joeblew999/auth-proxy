// Command dev holds this repo's developer tooling. It is not part of the proxy
// binary: mise tasks build and run it, so nothing here ships to users.
//
// Each tool is a package of its own, so what one owns is never entangled with
// another, and this file does nothing but parse arguments and say what is
// available. The rule for what lives here rather than in mise.toml: a task is
// a line or two of orchestration; anything with a branch, a loop, a parse or
// an API call is a subcommand here, where it has a test.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/bench"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/deps"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/mcpcheck"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/rel"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sessionpin"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sizes"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

const usage = `dev: developer tooling for this repo (run through mise).

  dev url [--worker] [--env NAME] [--local URL]   the URL to talk to: the deployed Worker, or --local
  dev worker deploy|wait|keys ...                 deploy from a throwaway config copy, wait for a Worker, secrets
  dev session sync|check|verify|bump              pin the Claude Code session to session.toml
  dev browser <server> <probe> [path]             serve an app, drive it in a headless Chrome, run the probe
  dev bench                                       Go vs TinyGo Worker in local workerd against the mock
  dev sizes FILE...                               raw and gzip sizes
  dev deps list|upgrade                           Go module upgrades in every module
  dev mcp check                                   every declared MCP server connects
  dev release snapshot|packslip|publish           publish a GitHub Release fully locally
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fail(args)
	}
	var err error
	switch args[0] {
	case "url":
		err = worker.RunURL(args[1:], os.Stdout, os.Stderr)
	case "worker":
		err = worker.Run(args[1:], os.Stdout, os.Stderr)
	case "browser":
		if len(args) < 3 {
			fail(args)
		}
		path := "/"
		if len(args) > 3 {
			path = args[3]
		}
		err = browser.Check(os.Stdout, args[1], args[2], path)
	case "session":
		err = sessionpin.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *sessionpin.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
	case "release":
		err = rel.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *rel.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
	case "bench":
		err = bench.Run(args[1:], os.Stdout, os.Stderr)
	case "sizes":
		err = sizes.Run(args[1:], os.Stdout, os.Stderr)
	case "deps":
		err = deps.Run(args[1:], os.Stdout, os.Stderr)
	case "mcp":
		err = mcpcheck.Run(args[1:], os.Stdout, os.Stderr)
	default:
		fail(args)
	}
	var werr *worker.UsageError
	if errors.As(err, &werr) {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, worker.Usage())
		os.Exit(2)
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
