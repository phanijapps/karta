//go:build !unix

package wardtools

// setupGroupKill is the non-unix fallback: only the direct process is
// killed (no process groups on the platform).
import "os/exec"

func setupGroupKill(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Kill() }
}
