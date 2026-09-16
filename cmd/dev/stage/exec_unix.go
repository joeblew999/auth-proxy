//go:build unix

package stage

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// execIn replaces this process with name, run in dir, so that a supervisor's
// signals reach the real process and no wrapper lingers.
func execIn(dir, name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not installed; mise install provides it", name)
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{name}, args...), os.Environ())
}
