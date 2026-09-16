//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the child in its own process group, so that stopping it also
// stops what it spawned: wrangler dev runs workerd as a child of its own.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stop(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_, _ = cmd.Process.Wait()
}
