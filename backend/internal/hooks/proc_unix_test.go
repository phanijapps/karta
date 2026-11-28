//go:build !windows

package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// TestTimeoutKillsTheWholeProcessGroup (gate F3): a compound command's
// child does not outlive the hook.
func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	t.Parallel()
	pidfile := filepath.Join(t.TempDir(), "pid")
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{
		"tree": {Event: domain.HookPreToolUse, Command: "sleep 30 & echo $! > " + pidfile + "; wait", TimeoutMS: 1000},
	})}
	d, err := r.Run(context.Background(), preTool("x"))
	if err != nil || d.Action != domain.HookWarn || !strings.Contains(d.Reason, "timed out") {
		t.Fatalf("the compound hook times out as a warning: %+v %v", d, err)
	}
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("the hook never wrote its child's pid: %v", err)
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &pid); err != nil || pid <= 0 {
		t.Fatalf("pid: %q %v", raw, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return // gone
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shell's child (pid %d) outlived the timed-out hook", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
