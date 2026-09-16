// Command dev is the stack's developer tool. It is not part of any binary that
// ships: mise tasks build and run it. A task names a stage of a command
// directory; dev reads the directory and does the rest, so a new command is
// new lines in mise.toml, never new tooling. Every package here is one thing,
// named as the tasks name it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/browser"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/deps"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/mcp"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/release"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/secrets"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/session"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/sizes"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/stage"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

const usage = `dev: the stack's developer tool (run through mise).

Stages of a command directory, read from what it holds (a Go main, a
package.json, .gsx sources, a wrangler.toml):
  dev build DIR                        npm, gsx generate, go build to bin/<dir>
  dev wasm DIR [--env NAME]            the Worker's wasm for the environment
  dev check DIR [--path P] [--expect TEXT]
                                       gsx fmt, vet, test, the workerd round trip, the browser probe
  dev run DIR [-- ARGS]                bin/<dir> under fnox
  dev workerd DIR [--env NAME]         the Worker on local workerd (wrangler dev)
  dev deploy DIR [--env NAME] [--wait PATH]
  dev url DIR [--worker] [--env NAME]
  dev logs DIR [--env NAME]
  dev smoke DIR [--path P] [--expect TEXT]
  dev secrets set DIR NAME|OWNER | push DIR
Stack verbs:
  dev session sync|check|verify|bump   pin the Claude Code session to session.toml
  dev mcp check                        every declared MCP server connects
  dev browser <server> <probe> [path]  serve an app, drive it in a headless Chrome, run the probe
  dev deps list|upgrade                Go module upgrades in every module
  dev release snapshot|packslip|publish
  dev sizes FILE...                    raw and gzip sizes
  dev wait URL                         until it answers 200 steadily
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fail(args)
	}
	var err error
	switch verb := args[0]; verb {
	case "build":
		fs := flagSet(verb)
		dir := dirOf(fs, args[1:])
		err = stage.Build(os.Stdout, dir, false, "")
	case "wasm":
		fs := flagSet(verb)
		env := fs.String("env", "", "wrangler environment")
		dir := dirOf(fs, args[1:])
		err = stage.Build(os.Stdout, dir, true, *env)
	case "check":
		fs := flagSet(verb)
		path := fs.String("path", "/", "what a Worker's smoke check and the browser probe request")
		expect := fs.String("expect", "", "text the smoke check's body must contain")
		dir := dirOf(fs, args[1:])
		err = stage.Check(os.Stdout, dir, *path, *expect)
	case "run":
		fs := flagSet(verb)
		dir := dirOf(fs, args[1:])
		err = stage.Run(dir, false, "", fs.Args())
	case "workerd":
		fs := flagSet(verb)
		env := fs.String("env", "", "wrangler environment")
		dir := dirOf(fs, args[1:])
		err = stage.Run(dir, true, *env, fs.Args())
	case "deploy", "url", "logs", "smoke", "wait":
		err = worker.Run(verb, args[1:], os.Stdout, os.Stderr)
		var uerr *worker.UsageError
		if errors.As(err, &uerr) {
			usageFail(err, worker.Usage())
		}
	case "secrets":
		err = secrets.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *secrets.UsageError
		if errors.As(err, &uerr) {
			usageFail(err, secrets.Usage())
		}
	case "session":
		err = session.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *session.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
	case "release":
		err = release.Run(args[1:], os.Stdout, os.Stderr)
		var uerr *release.UsageError
		if errors.As(err, &uerr) {
			fail(args)
		}
	case "browser":
		if len(args) < 3 {
			fail(args)
		}
		path := "/"
		if len(args) > 3 {
			path = args[3]
		}
		err = browser.Check(os.Stdout, args[1], args[2], path)
	case "mcp":
		err = mcp.Run(args[1:], os.Stdout, os.Stderr)
	case "deps":
		err = deps.Run(args[1:], os.Stdout, os.Stderr)
	case "sizes":
		err = sizes.Run(args[1:], os.Stdout, os.Stderr)
	default:
		fail(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func flagSet(name string) *flagSetT {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return &flagSetT{FlagSet: fs}
}

// dirOf reads `DIR [flags] [-- args]` for a stage; the directory comes first.
func dirOf(fs *flagSetT, args []string) string {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintf(os.Stderr, "dev %s: the command directory comes first, e.g. dev %s cmd/proxy\n\n", fs.Name(), fs.Name())
		fail(nil)
	}
	rest, err := cli.ParseInterleaved(fs.FlagSet, args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	fs.rest = rest
	return args[0]
}

func usageFail(err error, usage string) {
	fmt.Fprintln(os.Stderr, "error:", err)
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, usage)
	os.Exit(2)
}

func fail(args []string) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", strings.Join(args, " "))
	}
	fmt.Fprint(os.Stderr, usage)
	os.Exit(2)
}
