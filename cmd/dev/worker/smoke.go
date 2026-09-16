package worker

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Smoke runs the Worker in dir on local workerd through `wrangler dev`, asks
// it for path, and checks the answer: status 200, and the body containing
// expect when one is given. It is the round trip a Worker must make before
// anything is built on it, and it costs one wrangler dev start.
func Smoke(out io.Writer, dir, env, path, expect string, timeout time.Duration) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	logf, err := os.CreateTemp("", "wrangler-dev-*.log")
	if err != nil {
		return err
	}
	defer os.Remove(logf.Name())
	defer logf.Close()

	cmd := exec.Command("wrangler", "dev", "--env", env, "--ip", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	ownGroup(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting wrangler dev in %s: %w", dir, err)
	}
	defer stop(cmd)

	if err := waitReady(logf.Name(), timeout); err != nil {
		log, _ := os.ReadFile(logf.Name())
		fmt.Fprint(out, string(log))
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	code, size, err := check(url, expect)
	if err != nil {
		return fmt.Errorf("%s on workerd: %w", dir, err)
	}
	fmt.Fprintf(out, "%s on workerd answered %d for %s (%d bytes)\n", dir, code, path, size)
	return nil
}

// check fetches url and judges the answer.
func check(url, expect string) (code, size int, err error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, len(body), fmt.Errorf("%s answered %d: %s", url, resp.StatusCode, excerpt(body))
	}
	if expect != "" && !bytes.Contains(body, []byte(expect)) {
		return resp.StatusCode, len(body), fmt.Errorf("%s answered 200 but the body does not contain %q: %s", url, expect, excerpt(body))
	}
	return resp.StatusCode, len(body), nil
}

func excerpt(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// waitReady watches a wrangler dev log for readiness or an error.
func waitReady(log string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		data, _ := os.ReadFile(log)
		if bytes.Contains(data, []byte("ERROR")) {
			return fmt.Errorf("wrangler dev failed; its log is above")
		}
		if bytes.Contains(data, []byte("Ready on")) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wrangler dev did not become ready within %s; its log is above", timeout)
		}
		sleep(2 * time.Second)
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
