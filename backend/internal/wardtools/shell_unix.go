//go:build unix

package wardtools

// setupGroupKill arms whole-group termination for a ward shell command
// (unix): the command leads its own process group; cancellation kills
// the group, so backgrounded descendants die with the bound.
import (
	"os/exec"
	"syscall"
)

func setupGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck // best-effort group kill
		}
		return cmd.Process.Kill()
	}
}
