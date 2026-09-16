package worker

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The seams to fnox, wrangler and the network, as variables so tests can
// replace them. fnox holds every credential; wrangler only ever runs under
// `fnox exec`, which is how it gets CLOUDFLARE_API_TOKEN and
// CLOUDFLARE_ACCOUNT_ID without either touching a file in the repo.
var (
	// fnoxGet returns a secret's value, or an error when fnox does not have it.
	fnoxGet = func(name string) (string, error) {
		out, err := exec.Command("fnox", "get", name).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	// fnoxSet stores a value in the developer's global fnox config.
	fnoxSet = func(name, value string) error {
		cmd := exec.Command("fnox", "set", "-g", name)
		cmd.Stdin = strings.NewReader(value)
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// fnoxExec runs a command in dir with fnox's secrets in its environment.
	// wrangler finds the Worker's own wrangler.toml by running in its directory.
	fnoxExec = func(dir string, stdin io.Reader, stdout io.Writer, args ...string) error {
		cmd := exec.Command("fnox", append([]string{"exec", "--"}, args...)...)
		cmd.Dir = dir
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	httpClient = &http.Client{Timeout: 30 * time.Second}

	sleep = time.Sleep
)
