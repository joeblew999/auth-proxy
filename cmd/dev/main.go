// Command dev is the stack's developer tool. It is not part of any binary that
// ships: mise tasks build and run it. A task names a stage of a command
// directory; dev reads the directory and does the rest, so a new command is
// new lines in mise.toml, never new tooling. Every package is one thing, named
// as the tasks name it, and every verb has the one shape in internal/cli.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/deps"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/release"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/secrets"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/session"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/stage"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

// verbs is the whole tool: what each verb runs, and its usage.
var verbs = map[string]struct {
	run   cli.Runner
	usage string
}{
	"build":   {stage.Run, stage.Usage},
	"wasm":    {stage.Run, stage.Usage},
	"check":   {stage.Run, stage.Usage},
	"run":     {stage.Run, stage.Usage},
	"workerd": {stage.Run, stage.Usage},
	"deploy":  {worker.Run, worker.Usage},
	"url":     {worker.Run, worker.Usage},
	"logs":    {worker.Run, worker.Usage},
	"smoke":   {worker.Run, worker.Usage},
	"wait":    {worker.Run, worker.Usage},
	"secrets": {secrets.Run, secrets.Usage},
	"session": {session.Run, session.Usage},
	"release": {release.Run, release.Usage},
	"deps":    {deps.Run, deps.Usage},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, index())
		os.Exit(2)
	}
	verb, args := os.Args[1], os.Args[2:]
	v, ok := verbs[verb]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown verb %q\n\n%s", verb, index())
		os.Exit(2)
	}
	err := v.run(verb, args, os.Stdout, os.Stderr)
	var uerr *cli.UsageError
	if errors.As(err, &uerr) {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s", err, v.usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// index lists every verb, in the order the tool's packages are listed above.
func index() string {
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteString("dev: the stack's developer tool (run through mise). Verbs, by what does them:\n\n")
	var names []string
	for name := range verbs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		u := verbs[name].usage
		if seen[u] {
			continue
		}
		seen[u] = true
		b.WriteString(u)
		b.WriteString("\n")
	}
	return b.String()
}
