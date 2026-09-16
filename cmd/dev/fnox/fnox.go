// Package fnox is the one way dev reaches a secret. fnox holds every
// credential, and wrangler only ever runs under `fnox exec`, which is how it
// gets CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID without either touching
// a file in the repo. The functions are variables so tests can replace them.
package fnox

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

// Get returns a secret's value, or an error when fnox does not have it.
var Get = func(name string) (string, error) {
	out, err := exec.Command("fnox", "get", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Set stores a value in the developer's global fnox config. The value goes in
// on stdin, so it is never an argument, a process list or a shell history.
var Set = func(name, value string) error {
	cmd := exec.Command("fnox", "set", "-g", name)
	cmd.Stdin = strings.NewReader(value)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Exec runs a command in dir with fnox's secrets in its environment.
var Exec = func(dir string, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := exec.Command("fnox", append([]string{"exec", "--"}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
