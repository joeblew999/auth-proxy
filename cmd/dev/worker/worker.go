// Package worker is the Cloudflare Worker side of the developer workflow: this
// clone's Worker URL, a deploy that leaves no personal value in git, secrets,
// and waiting for a Worker to come online. It runs as `dev url` and
// `dev worker ...` through cmd/dev.
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

const usage = `dev url [--worker[=BOOL]] [--env NAME] [--local URL] [--refresh]
    print the URL to talk to: the deployed Worker when --worker, else --local
    (default empty). The account's workers.dev subdomain is read once with the
    credentials in fnox and kept in gitignored mise.local.toml; --refresh asks
    again, for after switching accounts.

dev worker deploy [--env NAME]
    deploy from a throwaway copy of wrangler.toml, so the ids wrangler writes
    back into its config never reach git, then say what was created or that
    the deployed Worker's bindings were inherited
dev worker wait URL [--timeout DURATION]
    wait until URL answers 200 (a first deploy's hostname takes a while)
dev worker keys set NAME [--generate] [--if-missing] [--env NAME]
    store a secret in fnox and push it to the Worker; --generate makes a random
    value instead of prompting, --if-missing leaves an existing one alone
dev worker keys push [--env NAME] [--fix TEMPLATE]
    read "NAME<TAB>PROVIDER" lines on stdin and push each secret from fnox to
    the Worker; a missing one prints TEMPLATE with {provider} filled in, and
    any problem makes the exit code 1

Run from the repo root. Needs wrangler.toml, fnox and wrangler.
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

// RunURL is `dev url`.
func RunURL(args []string, stdout, stderr io.Writer) error {
	fs := flags("url", stderr)
	var worker, refresh cli.Bool
	fs.Var(&worker, "worker", "the deployed Worker's URL; otherwise --local")
	fs.Var(&refresh, "refresh", "ask the API again instead of reading mise.local.toml")
	env := fs.String("env", "", "wrangler environment; its Worker is <name>-<env> unless it sets a name")
	local := fs.String("local", "", "what to print when not --worker")
	if err := fs.Parse(args); err != nil {
		return usageErr("url: %v", err)
	}
	if fs.NArg() != 0 {
		return usageErr("url takes no arguments")
	}
	u, err := URL(*env, bool(worker), *local, bool(refresh))
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, u)
	return nil
}

// Run is `dev worker ...`. Args are everything after "worker".
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageErr("worker: missing subcommand")
	}
	switch args[0] {
	case "deploy":
		fs := flags("worker deploy", stderr)
		env := fs.String("env", "", "wrangler environment to deploy")
		if err := fs.Parse(args[1:]); err != nil {
			return usageErr("worker deploy: %v", err)
		}
		if fs.NArg() != 0 {
			return usageErr("worker deploy takes no arguments")
		}
		return Deploy(stdout, *env)
	case "wait":
		fs := flags("worker wait", stderr)
		timeout := fs.Duration("timeout", 2*time.Minute, "how long to keep trying")
		if err := fs.Parse(args[1:]); err != nil {
			return usageErr("worker wait: %v", err)
		}
		if fs.NArg() != 1 {
			return usageErr("worker wait needs exactly one URL")
		}
		return Wait(stdout, fs.Arg(0), *timeout)
	case "keys":
		return runKeys(args[1:], stdout, stderr)
	}
	return usageErr("worker: unknown subcommand %q", args[0])
}

func runKeys(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageErr("worker keys: missing subcommand")
	}
	switch args[0] {
	case "set":
		fs := flags("worker keys set", stderr)
		var generate, ifMissing cli.Bool
		fs.Var(&generate, "generate", "make a random 64-hex-character value instead of prompting")
		fs.Var(&ifMissing, "if-missing", "do nothing when fnox already has the secret")
		env := fs.String("env", "", "wrangler environment to push to")
		if err := fs.Parse(args[1:]); err != nil {
			return usageErr("worker keys set: %v", err)
		}
		if fs.NArg() != 1 || fs.Arg(0) == "" {
			return usageErr("worker keys set needs exactly one secret name")
		}
		return KeysSet(os.Stdin, stdout, stderr, fs.Arg(0), bool(generate), bool(ifMissing), *env)
	case "push":
		fs := flags("worker keys push", stderr)
		env := fs.String("env", "", "wrangler environment to push to")
		fix := fs.String("fix", "mise run keys:set {provider}", "what to run for a secret fnox does not have")
		if err := fs.Parse(args[1:]); err != nil {
			return usageErr("worker keys push: %v", err)
		}
		if fs.NArg() != 0 {
			return usageErr("worker keys push reads its list on stdin and takes no arguments")
		}
		return KeysPush(os.Stdin, stdout, *env, *fix)
	}
	return usageErr("worker keys: unknown subcommand %q", args[0])
}
