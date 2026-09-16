// Package secrets stores a Worker's secrets in fnox and pushes them to the
// Worker with wrangler. Names arrive on the command line or stdin; values
// only ever pass through fnox and wrangler. It runs as `dev secrets ...`.
package secrets

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/fnox"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/internal/cli"
	"github.com/joeblew999/grok-oauth-proxy/cmd/dev/worker"
)

const Usage = `dev secrets set DIR NAME|OWNER [--names LIST] [--generate] [--if-missing] [--env NAME]
    store a secret in fnox and push it to the Worker in DIR; --generate makes a
    random value instead of prompting, --if-missing leaves an existing one
    alone. With --names, the project's "NAME<TAB>OWNER" lines, an owner such as
    a provider name resolves to its secret
dev secrets push DIR [--env NAME] [--fix TEMPLATE]
    read "NAME<TAB>OWNER" lines on stdin and push each secret from fnox to the
    Worker in DIR; a missing one prints TEMPLATE with {provider} filled in, and
    any problem makes the exit code 1
`

// Run is `dev secrets set|push`.
func Run(verb string, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return cli.Usagef("secrets: set or push")
	}
	fs := cli.Flags("secrets "+args[0], stderr)
	env := fs.String("env", "", "wrangler environment to push to")
	switch args[0] {
	case "set":
		var generate, ifMissing cli.Bool
		fs.Var(&generate, "generate", "make a random 64-hex-character value instead of prompting")
		fs.Var(&ifMissing, "if-missing", "do nothing when fnox already has the secret")
		names := fs.String("names", "", "the project's NAME<TAB>OWNER lines, so an owner resolves to its secret")
		dir, rest, err := cli.DirAnd(fs, args[1:], 1)
		if err != nil {
			return err
		}
		name, err := Resolve(*names, rest[0])
		if err != nil {
			return err
		}
		return Set(os.Stdin, stdout, stderr, name, bool(generate), bool(ifMissing), dir, *env)
	case "push":
		fix := fs.String("fix", "mise run secrets:set {provider}", "what to run for a secret fnox does not have")
		dir, _, err := cli.DirAnd(fs, args[1:], 0)
		if err != nil {
			return err
		}
		return Push(os.Stdin, stdout, dir, *env, *fix)
	}
	return cli.Usagef("secrets: unknown subcommand %q", args[0])
}

// Resolve turns what the developer typed into a secret name, given the
// project's "NAME<TAB>OWNER" lines: an owner (a provider, "admin") maps to its
// name, a name maps to itself, anything else is an error naming what exists.
func Resolve(names, arg string) (string, error) {
	var known []string
	for _, line := range strings.Split(names, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		known = append(known, fields[0])
		if fields[0] == arg || (len(fields) > 1 && fields[1] == arg) {
			return fields[0], nil
		}
	}
	if strings.TrimSpace(names) == "" {
		return arg, nil
	}
	return "", fmt.Errorf("%q is not a secret or an owner this project knows; the secrets are: %s", arg, strings.Join(known, ", "))
}

// Set stores one secret in fnox and pushes it to the Worker. The value is
// generated, or read hidden from a terminal, or read as one line from a pipe.
func Set(stdin io.Reader, stdout, stderr io.Writer, name string, generate, ifMissing bool, dir, env string) error {
	if ifMissing {
		if v, err := fnox.Get(name); err == nil && v != "" {
			fmt.Fprintf(stdout, "%s is already in fnox; leaving it\n", name)
			return nil
		}
	}
	value, err := value(stdin, stderr, name, generate)
	if err != nil {
		return err
	}
	if err := fnox.Set(name, value); err != nil {
		return fmt.Errorf("storing %s in fnox: %w", name, err)
	}
	fmt.Fprintf(stdout, "Stored %s in fnox.\n", name)
	if err := push(dir, name, value, env); err != nil {
		return fmt.Errorf("pushing %s to the Worker: %w (retry with: mise run secrets:push)", name, err)
	}
	target, _ := worker.Name(dir, env)
	fmt.Fprintf(stdout, "Pushed %s to %s.\n", name, target)
	return nil
}

func value(stdin io.Reader, prompt io.Writer, name string, generate bool) (string, error) {
	if generate {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil
	}
	var v string
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(prompt, "Value for %s (input hidden): ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		v = string(b)
	} else {
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		v = strings.TrimRight(line, "\r\n")
	}
	if v == "" {
		return "", fmt.Errorf("no value given for %s; nothing was changed", name)
	}
	return v, nil
}

// push is `wrangler secret put`, which reads the value on stdin and deploys a
// new version of the Worker with it. The Worker is named, so a developer's
// suffixed copy gets its own secrets.
func push(dir, name, value, env string) error {
	target, err := worker.Name(dir, env)
	if err != nil {
		return err
	}
	return fnox.Exec(dir, strings.NewReader(value), io.Discard, "wrangler", "secret", "put", name, "--env", env, "--name", target)
}

// Push reads "NAME<TAB>OWNER" lines (owner optional) and pushes every named
// secret from fnox to the Worker. A secret fnox does not have is reported
// with fix, {provider} replaced, and the error at the end carries the count
// so the caller exits non-zero.
func Push(stdin io.Reader, out io.Writer, dir, env, fix string) error {
	problems := 0
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		name, owner := fields[0], fields[0]
		if len(fields) > 1 {
			owner = fields[1]
		}
		v, err := fnox.Get(name)
		if err != nil || v == "" {
			fmt.Fprintf(out, "missing %s -> %s\n", name, strings.ReplaceAll(fix, "{provider}", owner))
			problems++
			continue
		}
		if err := push(dir, name, v, env); err != nil {
			fmt.Fprintf(out, "failed  %s (%v; retry with: mise run secrets:push)\n", name, err)
			problems++
			continue
		}
		fmt.Fprintf(out, "pushed  %s\n", name)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if problems > 0 {
		return fmt.Errorf("%d secret(s) not pushed", problems)
	}
	return nil
}
