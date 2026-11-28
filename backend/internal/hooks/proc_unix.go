//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

// hookProcAttr puts the hook in its own process group so a timeout can
// kill the whole tree (a compound command's children included).
func hookProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup terminates the hook's process group (SIGKILL, then reap).
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck // best-effort teardown
	}
}
