package worker

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Resolve turns what the developer typed into a secret name, given the
// project's list of "NAME<TAB>OWNER" lines: an owner (a provider, "admin")
// maps to its name, a name maps to itself, anything else is an error naming
// what exists.
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

// KeysSet stores one secret in fnox and pushes it to the Worker. The value is
// generated, or read hidden from a terminal, or read as one line from a pipe;
// it is never an argument, so it is never in a process list or a shell history.
func KeysSet(stdin io.Reader, stdout, stderr io.Writer, name string, generate, ifMissing bool, dir, env string) error {
	if ifMissing {
		if v, err := fnoxGet(name); err == nil && v != "" {
			fmt.Fprintf(stdout, "%s is already in fnox; leaving it\n", name)
			return nil
		}
	}
	value, err := secretValue(stdin, stderr, name, generate)
	if err != nil {
		return err
	}
	if err := fnoxSet(name, value); err != nil {
		return fmt.Errorf("storing %s in fnox: %w", name, err)
	}
	fmt.Fprintf(stdout, "Stored %s in fnox.\n", name)
	if err := pushSecret(dir, name, value, env); err != nil {
		return fmt.Errorf("pushing %s to the Worker: %w (retry with: mise run keys:push)", name, err)
	}
	fmt.Fprintf(stdout, "Pushed %s to the Worker. Check with: mise run status --worker\n", name)
	return nil
}

func secretValue(stdin io.Reader, prompt io.Writer, name string, generate bool) (string, error) {
	if generate {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil
	}
	var value string
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(prompt, "Value for %s (input hidden): ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		value = string(b)
	} else {
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		value = strings.TrimRight(line, "\r\n")
	}
	if value == "" {
		return "", fmt.Errorf("no value given for %s; nothing was changed", name)
	}
	return value, nil
}

// pushSecret is `wrangler secret put`, which reads the value on stdin and
// deploys a new version of the Worker with it.
func pushSecret(dir, name, value, env string) error {
	return fnoxExec(dir, strings.NewReader(value), io.Discard, "wrangler", "secret", "put", name, "--env", env)
}

// KeysPush reads "NAME<TAB>PROVIDER" lines (provider optional) and pushes
// every named secret from fnox to the Worker. A secret fnox does not have is
// reported with fix, {provider} replaced, and the error at the end carries the
// count so the caller exits non-zero.
func KeysPush(stdin io.Reader, out io.Writer, dir, env, fix string) error {
	problems := 0
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		name, provider := fields[0], fields[0]
		if len(fields) > 1 {
			provider = fields[1]
		}
		value, err := fnoxGet(name)
		if err != nil || value == "" {
			fmt.Fprintf(out, "missing %s -> %s\n", name, strings.ReplaceAll(fix, "{provider}", provider))
			problems++
			continue
		}
		if err := pushSecret(dir, name, value, env); err != nil {
			fmt.Fprintf(out, "failed  %s (%v; see mise run logs, or retry mise run keys:push)\n", name, err)
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
