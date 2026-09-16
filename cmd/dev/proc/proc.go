// Package proc starts a child in its own process group and stops the whole
// group, so that stopping wrangler dev also stops the workerd it spawned.
// Windows has no process groups; there the child alone is killed.
package proc

import "os/exec"

// OwnGroup makes cmd start in its own process group. Call it before Start.
func OwnGroup(cmd *exec.Cmd) { ownGroup(cmd) }

// Stop terminates cmd's whole process group and waits for it.
func Stop(cmd *exec.Cmd) { stop(cmd) }
