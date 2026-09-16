// Package worker is the Cloudflare Worker side of the developer workflow: a
// Worker's URL for this clone, a deploy that leaves no personal value in git,
// its logs, a smoke round trip on workerd, and waiting for it to come online.
// Every command takes the Worker's directory, the one holding its
// wrangler.toml, so a second Worker is a second directory and nothing else.
package worker

import (
	"fmt"
	"io"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
)

const Usage = `dev url DIR [--worker[=BOOL]] [--env NAME] [--local URL] [--refresh]
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

Run from the repo root. Needs fnox and wrangler.
`

// Run is every Worker verb. DIR comes first; flags may follow anywhere.
func Run(verb string, args []string, stdout, stderr io.Writer) error {
	fs := cli.Flags(verb, stderr)
	env := fs.String("env", "", "wrangler environment")
	switch verb {
	case "url":
		var asWorker, refresh cli.Bool
		fs.Var(&asWorker, "worker", "the deployed Worker's URL; otherwise --local")
		fs.Var(&refresh, "refresh", "ask the API again instead of reading mise.local.toml")
		local := fs.String("local", "", "what to print when not --worker")
		dir, _, err := cli.DirAnd(fs, args, 0)
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
		wait := fs.String("wait", "", "path to wait for a 200 on after deploying, e.g. /health")
		dir, _, err := cli.DirAnd(fs, args, 0)
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
		dir, _, err := cli.DirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		return Logs(dir, *env)
	case "smoke":
		path := fs.String("path", "/", "what to request")
		expect := fs.String("expect", "", "text the body must contain")
		timeout := fs.Duration("timeout", 3*time.Minute, "how long wrangler dev may take to start")
		dir, _, err := cli.DirAnd(fs, args, 0)
		if err != nil {
			return err
		}
		return Smoke(stdout, dir, *env, *path, *expect, *timeout)
	case "wait":
		timeout := fs.Duration("timeout", 2*time.Minute, "how long to keep trying")
		if err := fs.Parse(args); err != nil {
			return cli.Usagef("wait: %v", err)
		}
		if fs.NArg() != 1 {
			return cli.Usagef("wait needs exactly one URL")
		}
		return Wait(stdout, fs.Arg(0), *timeout)
	}
	return cli.Usagef("worker: unknown verb %q", verb)
}
