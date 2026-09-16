package worker

import (
	"os"
	"os/exec"
)

// Logs streams the deployed Worker's logs, in the Worker's directory so
// wrangler finds its config, under fnox for the account. It runs in the
// foreground until interrupted.
func Logs(dir, env string) error {
	cmd := exec.Command("fnox", "exec", "--", "wrangler", "tail", "--env", env)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
