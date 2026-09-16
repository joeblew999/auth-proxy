//go:build !unix

package main

import "os/exec"

func ownGroup(*exec.Cmd) {}

func stop(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}
