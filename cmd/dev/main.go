// Command dev holds the stack's developer tooling. It is not part of any
// binary that ships: mise tasks build and run it. mise names a stage per
// command directory; dev reads the directory and does the rest, so a new
// command is new lines in mise.toml, never new tooling.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/app"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/deps"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/mcpcheck"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/rel"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sessionpin"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sizes"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

const usage = `dev: the stack's developer tool (run through mise).

Stages of a command directory, read from what it holds (Go main, package.json,
.gsx, wrangler.toml):
  dev build DIR [--env NAME]                     npm, gsx generate, go build to bin/<dir>, a Worker's wasm
  dev run DIR [--worker] [--env NAME] [-- ARGS]  bin/<dir> under fnox, or wrangler dev
  dev check DIR [--path P] [--expect TEXT]       gsx fmt, vet, test, a Worker's workerd round trip, the browser probe
Worker stages (DIR holds the wrangler.toml):
  dev deploy DIR [--env NAME] [--wait PATH]      deploy from a throwaway config copy, report, wait
  dev url DIR [--worker] [--env NAME]            the URL to talk to
  dev logs DIR [--env NAME]                      wrangler tail
  dev smoke DIR [--path P] [--expect TEXT]       wrangler dev, one request, checked
  dev wait URL                                   until it answers 200 steadily
  dev keys set DIR NAME|OWNER | push DIR         secrets through fnox and wrangler
The rest:
  dev session sync|check|verify|bump             pin the Claude Code session to session.toml
  dev browser <server> <probe> [path]            serve an app, drive it in a headless Chrome, run the probe
  dev sizes FILE...                              raw and gzip sizes
  dev deps list|upgrade                          Go module upgrades in every module
  dev mcp check                                  every declared MCP server connects
  dev release snapshot|packslip|publish          publish a GitHub Release fully locally
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fail(args)
	}
	var err error
	switch args[0] {
	case "build":
		fs := stageFlags("build")
		env := fs.String("env", "", "wrangler environment to build the Worker for")
		dir := parseDir(fs, args[1:])
		err = app.Build(os.Stdout, dir, *env)
	case "run":
		fs := stageFlags("run")
		var asWorker cli.Bool
		fs.Var(&asWorker, "worker", "run the Worker on local workerd instead of the binary")
		env := fs.String("env", "", "wrangler environment to run")
		dir := parseDir(fs, args[1:])
		err = app.Run(dir, bool(asWorker), *env, fs.Args())
	case "check":
		fs := stageFlags("check")
		path := fs.String("path", "/", "what a Worker's smoke check and the browser probe request")
		expect := fs.String("expect", "", "text the smoke check's body must contain")
		dir := parseDir(fs, args[1:])
		err = app.Check(os.Stdout, dir, *path, *expect)
	case "url", "deploy", "logs", "smoke", "wait", "keys":
		err = worker.Run(args[0], args[1:], os.Stdout, os.Stderr)
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

// A stage is `dev <stage> DIR [flags] [-- args]`.
func stageFlags(name string) *flagSet {
	fs := &flagSet{}
	fs.FlagSet = newFlagSet(name)
	return fs
}

func parseDir(fs *flagSet, args []string) string {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintf(os.Stderr, "dev %s: the command directory comes first, e.g. dev %s cmd/worker\n\n", fs.Name(), fs.Name())
		fail(nil)
	}
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	return args[0]
}

func fail(args []string) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", strings.Join(args, " "))
	}
	fmt.Fprint(os.Stderr, usage)
	os.Exit(2)
}
