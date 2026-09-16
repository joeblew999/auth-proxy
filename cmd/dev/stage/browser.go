// The browser probe: a server-rendered page can be checked with curl, but a
// page whose point is client-side behaviour (a picker that swaps models with
// no round trip) cannot. That needs a real browser.

package stage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// chromePaths are where a headless-capable browser usually lives, per OS. The
// CHROME environment variable wins over all of them.
var chromePaths = map[string][]string{
	"darwin": {
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	},
	"linux": {
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	},
}

// errNoChrome means no browser was found. The check reports it and passes,
// rather than failing a developer who has no Chrome installed; the message says
// what to do about it.
var errNoChrome = errors.New("no Chrome found")

// findChrome returns the browser to drive.
func findChrome(getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) (string, error) {
	if set := getenv("CHROME"); set != "" {
		if !exists(set) {
			return "", fmt.Errorf("CHROME is set to %q, which does not exist", set)
		}
		return set, nil
	}
	for _, path := range chromePaths[runtime.GOOS] {
		if exists(path) {
			return path, nil
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := lookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errNoChrome
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// probe builds nothing: the stage has already built server. It serves the app
// on a free port, points a headless Chrome at it, and runs the probe script
// under node.
func probe(out io.Writer, server, probe, path string) error {
	chrome, err := findChrome(os.Getenv, exec.LookPath, fileExists)
	if errors.Is(err, errNoChrome) {
		fmt.Fprintf(out, "SKIPPED: no Chrome found, so the browser checks did not run.\n"+
			"Install Google Chrome or Chromium, or point at one with CHROME=/path/to/chrome and run the check again.\n")
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node is not on PATH; run this through mise, which pins it")
	}

	appPort, err := freePort()
	if err != nil {
		return err
	}
	app := exec.Command(server)
	app.Env = append(os.Environ(), fmt.Sprintf("GO_PORT=%d", appPort))
	app.Stdout, app.Stderr = io.Discard, os.Stderr
	if err := app.Start(); err != nil {
		return fmt.Errorf("start %s: %w", server, err)
	}
	defer stop(app)

	url := fmt.Sprintf("http://127.0.0.1:%d%s", appPort, path)
	if err := waitFor(url, 15*time.Second); err != nil {
		return fmt.Errorf("%s never answered: %w", server, err)
	}

	debugPort, err := freePort()
	if err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", "browser-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)

	browser := exec.Command(chrome,
		"--headless",
		"--disable-gpu",
		"--no-first-run",
		"--user-data-dir="+profile,
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"about:blank",
	)
	browser.Stdout, browser.Stderr = io.Discard, io.Discard
	if err := browser.Start(); err != nil {
		return fmt.Errorf("start %s: %w", chrome, err)
	}
	defer stop(browser)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", debugPort)
	if err := waitFor(endpoint+"/json/list", 20*time.Second); err != nil {
		return fmt.Errorf("%s never opened its debugging port: %w", filepath.Base(chrome), err)
	}

	fmt.Fprintf(out, "%s driving %s\n\n", filepath.Base(chrome), url)
	node := exec.Command("node", probe, endpoint, url)
	node.Stdout, node.Stderr = out, os.Stderr
	return node.Run()
}

// freePort asks the kernel for a port nobody is using, so two developers (or
// two tasks) can run this at the same time.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitFor(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			return nil
		}
		last = err
		time.Sleep(100 * time.Millisecond)
	}
	return last
}

func stop(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}
