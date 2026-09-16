// Package worker is the Cloudflare Worker side of the developer workflow: a
// Worker's URL for this clone, a deploy that leaves no personal value in git,
// its logs, its secrets, a smoke round trip on workerd, and waiting for it to
// come online. Every command takes the Worker's directory, the one holding
// its wrangler.toml, so a second Worker is a second directory and nothing else.
//
// Nothing here knows the project's providers or secrets. Names arrive on the
// command line or stdin; values only ever pass through fnox and wrangler.
package worker

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
)

const usage = `dev url DIR [--worker[=BOOL]] [--env NAME] [--local URL] [--refresh]
    print the URL to talk to: the Worker in DIR when --worker, else --local
    (default empty). The account's workers.dev subdomain is read once with the
    credentials in fnox and kept in gitignored mise.local.toml; --refresh asks
    again, for after switching accounts.
dev deploy DIR [--env NAME] [--wait PATH]
    deploy from a throwaway copy of DIR/wrangler.toml, so the ids wrangler
    writes back never reach git; say what was created or inherited; with
    --wait, wait until the Worker answers 200 at PATH
dev logs DIR [--env NAME]
    stream the deployed Worker's logs (wrangler tail)
dev smoke DIR [--env NAME] [--path P] [--expect TEXT] [--timeout DURATION]
    run the Worker on local workerd with wrangler dev, request P (default /),
    and fail unless it answers 200 with TEXT in the body
dev wait URL [--timeout DURATION]
    wait until URL answers 200 steadily
dev keys set DIR NAME|OWNER [--names LIST] [--generate] [--if-missing] [--env NAME]
    store a secret in fnox and push it to the Worker; --generate makes a random
    value instead of prompting, --if-missing leaves an existing one alone. With
    --names, the project's "NAME<TAB>OWNER" lines, an owner such as a provider
    name resolves to its secret
dev keys push DIR [--env NAME] [--fix TEMPLATE]
    read "NAME<TAB>OWNER" lines on stdin and push each secret from fnox to the
    Worker; a missing one prints TEMPLATE with {provider} filled in, and any
    problem makes the exit code 1

Run from the repo root. Needs fnox and wrangler.
`

// UsageError means the arguments were wrong; cmd/dev prints usage on it.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usageErr(format string, a ...any) error {
	return &UsageError{fmt.Sprintf(format, a...)}
}

// Usage is the help text cmd/dev prints for these commands.
func Usage() string { return usage }

func flags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// dirAnd parses "DIR [flags]" from args: the directory first, then flags.
func dirAnd(fs *flag.FlagSet, args []string, positional int) (dir string, rest []string, err error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return "", nil, usageErr("%s: the Worker's directory comes first", fs.Name())
	}
	rest, err = parseInterleaved(fs, args[1:])
	if err != nil {
		return "", nil, usageErr("%s: %v", fs.Name(), err)
	}
	if len(rest) != positional {
		return "", nil, usageErr("%s: wrong arguments", fs.Name())
	}
	return args[0], rest, nil
}

// parseInterleaved parses flags wherever they appear, returning the
// positionals. mise appends what the developer typed after the task name to
// the command, so `keys set DIR admin --generate` must work.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positionals = append(positionals, args[0])
			args = args[1:]
		}
	}
	return positionals, nil
}

// Run dispatches the Worker commands. Args are everything after the verb,
// which is passed as verb.
func Run(verb string, args []string, stdout, stderr io.Writer) error {
	switch verb {
	case "url":
		fs := flags("url", stderr)
		var asWorker, refresh cli.Bool
		fs.Var(&asWorker, "worker", "the deployed Worker's URL; otherwise --local")
		fs.Var(&refresh, "refresh", "ask the API again instead of reading mise.local.toml")
		env := fs.String("env", "", "wrangler environment; its Worker is <name>-<env> unless it sets a name")
		local := fs.String("local", "", "what to print when not --worker")
		dir, _, err := dirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		u, err := URL(dir, *env, bool(asWorker), *local, bool(refresh))
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, u)
		return nil
	case "deploy":
		fs := flags("deploy", stderr)
		env := fs.String("env", "", "wrangler environment to deploy")
		wait := fs.String("wait", "", "path to wait for a 200 on after deploying, e.g. /health")
		dir, _, err := dirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		if err := Deploy(stdout, dir, *env); err != nil {
			return err
		}
		if *wait == "" {
			return nil
		}
		u, err := URL(dir, *env, true, "", false)
		if err != nil {
			return err
		}
		return Wait(stdout, u+*wait, 2*time.Minute)
	case "logs":
		fs := flags("logs", stderr)
		env := fs.String("env", "", "wrangler environment")
		dir, _, err := dirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		return Logs(dir, *env)
	case "smoke":
		fs := flags("smoke", stderr)
		env := fs.String("env", "", "wrangler environment to run")
		path := fs.String("path", "/", "what to request")
		expect := fs.String("expect", "", "text the body must contain")
		timeout := fs.Duration("timeout", 3*time.Minute, "how long wrangler dev may take to start")
		dir, _, err := dirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		return Smoke(stdout, dir, *env, *path, *expect, *timeout)
	case "wait":
		fs := flags("wait", stderr)
		timeout := fs.Duration("timeout", 2*time.Minute, "how long to keep trying")
		if err := fs.Parse(args); err != nil {
			return usageErr("wait: %v", err)
		}
		if fs.NArg() != 1 {
			return usageErr("wait needs exactly one URL")
		}
		return Wait(stdout, fs.Arg(0), *timeout)
	case "keys":
		return runKeys(args, stdout, stderr)
	}
	return usageErr("unknown command %q", verb)
}

func runKeys(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageErr("keys: set or push")
	}
	switch args[0] {
	case "set":
		fs := flags("keys set", stderr)
		var generate, ifMissing cli.Bool
		fs.Var(&generate, "generate", "make a random 64-hex-character value instead of prompting")
		fs.Var(&ifMissing, "if-missing", "do nothing when fnox already has the secret")
		env := fs.String("env", "", "wrangler environment to push to")
		names := fs.String("names", "", "the project's NAME<TAB>OWNER lines, so an owner resolves to its secret")
		dir, rest, err := dirAnd(fs, args[1:], 1)
		if err != nil {
			return err
		}
		name, err := Resolve(*names, rest[0])
		if err != nil {
			return err
		}
		return KeysSet(os.Stdin, stdout, stderr, name, bool(generate), bool(ifMissing), dir, *env)
	case "push":
		fs := flags("keys push", stderr)
		env := fs.String("env", "", "wrangler environment to push to")
		fix := fs.String("fix", "mise run keys:set {provider}", "what to run for a secret fnox does not have")
		dir, _, err := dirAnd(fs, args[1:], 0)
		if err != nil {
			return err
		}
		return KeysPush(os.Stdin, stdout, dir, *env, *fix)
	}
	return usageErr("keys: unknown subcommand %q", args[0])
}
