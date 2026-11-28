package hooks

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// manifest writes nothing to disk: the runner only needs the command and
// the manifest's directory (cwd).
func snap(t *testing.T, hooks map[string]registry.HookSpec) func() registry.Snapshot {
	t.Helper()
	dir := t.TempDir()
	for k, h := range hooks {
		h.Path = filepath.Join(dir, k+".yaml")
		hooks[k] = h
	}
	return func() registry.Snapshot { return registry.Snapshot{Hooks: hooks} }
}

func preTool(tool string) domain.HookEvent {
	return domain.HookEvent{Event: domain.HookPreToolUse, SessionID: "s1", ToolName: tool, ToolInput: map[string]any{"n": 7}}
}

func TestExitCodeContract(t *testing.T) {
	t.Parallel()
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{
		"a-allow": {Event: domain.HookPreToolUse, Command: "exit 0"},
		"b-block": {Event: domain.HookPreToolUse, Command: "echo 'no ward writes' >&2; exit 2"},
		"c-warn":  {Event: domain.HookPreToolUse, Command: "echo 'slow today' >&2; exit 3"},
	})}
	d, err := r.Run(context.Background(), preTool("ward_write"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != domain.HookBlock || !strings.Contains(d.Reason, "b-block: no ward writes") || !strings.Contains(d.Reason, "c-warn: slow today") || strings.Contains(d.Reason, "a-allow") {
		t.Fatalf("block wins with per-hook reasons: %+v", d)
	}
	if ran, _ := d.Context["hooks"].([]string); strings.Join(ran, ",") != "a-allow,b-block,c-warn" {
		t.Fatalf("context names the hooks that ran in key order: %v", d.Context)
	}
	if d.Context["event"] != "PreToolUse" || d.Context["subject"] != "ward_write" {
		t.Fatalf("context carries the event and subject: %v", d.Context)
	}
	// Warn-only and allow-only rosters.
	r2 := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{"w": {Event: domain.HookPreToolUse, Command: "exit 1"}})}
	if d, _ := r2.Run(context.Background(), preTool("x")); d.Action != domain.HookWarn || !strings.Contains(d.Reason, "w: exit status 1") {
		t.Fatalf("a non-zero, non-2 exit warns with sh's message: %+v", d)
	}
	r3 := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{"ok": {Event: domain.HookPreToolUse, Command: "echo fine"}})}
	if d, _ := r3.Run(context.Background(), preTool("x")); d.Action != domain.HookAllow || d.Reason != "" {
		t.Fatalf("an allow carries no reason: %+v", d)
	}
	// A block with empty stderr still names its hook.
	r4 := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{"silent": {Event: domain.HookPreToolUse, Command: "exit 2"}})}
	if d, _ := r4.Run(context.Background(), preTool("x")); d.Action != domain.HookBlock || d.Reason != "silent: blocked by hook silent" {
		t.Fatalf("empty stderr: %+v", d)
	}
}

func TestTimeoutIsAWarning(t *testing.T) {
	t.Parallel()
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{"slow": {Event: domain.HookPreToolUse, Command: "sleep 30", TimeoutMS: 500}})}
	start := time.Now()
	d, err := r.Run(context.Background(), preTool("x"))
	if err != nil || d.Action != domain.HookWarn || !strings.Contains(d.Reason, "slow: timed out after") {
		t.Fatalf("a timeout warns and names the hook: %+v %v", d, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the hook must be killed at its (clamped) timeout: %s", time.Since(start))
	}
}

func TestMatcherSelectsBySubject(t *testing.T) {
	t.Parallel()
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{
		"ward-only":   {Event: domain.HookPreToolUse, Matcher: regexp.MustCompile(`^ward_`), Command: "exit 2"},
		"research":    {Event: domain.HookSubagentStop, Matcher: regexp.MustCompile(`^research`), Command: "exit 2"},
		"compact-any": {Event: domain.HookPreCompact, Matcher: regexp.MustCompile(`^never`), Command: "exit 2"},
	})}
	if d, _ := r.Run(context.Background(), preTool("clock.now")); d.Action != domain.HookAllow {
		t.Fatalf("the ward matcher skips clock.now: %+v", d)
	}
	if d, _ := r.Run(context.Background(), preTool("ward_write")); d.Action != domain.HookBlock {
		t.Fatalf("the ward matcher matches ward_write: %+v", d)
	}
	if d, _ := r.Run(context.Background(), domain.HookEvent{Event: domain.HookSubagentStop, Payload: map[string]any{"agent": "builder-agent"}}); d.Action != domain.HookAllow {
		t.Fatalf("the research matcher skips the builder: %+v", d)
	}
	if d, _ := r.Run(context.Background(), domain.HookEvent{Event: domain.HookSubagentStop, Payload: map[string]any{"agent": "research-agent"}}); d.Action != domain.HookBlock {
		t.Fatalf("the research matcher matches the researcher: %+v", d)
	}
	// Subject-less: the matcher is ignored, the hook runs.
	if d, _ := r.Run(context.Background(), domain.HookEvent{Event: domain.HookPreCompact}); d.Action != domain.HookBlock {
		t.Fatalf("a matcher on a subject-less event is ignored: %+v", d)
	}
}

func TestHookReadsTheEventOnStdin(t *testing.T) {
	t.Parallel()
	// The hook decides from the event JSON: block only when the argument n is 7.
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{"gate": {Event: domain.HookPreToolUse, Command: `grep -q '"n":7' && exit 2 || exit 0`}})}
	if d, _ := r.Run(context.Background(), preTool("x")); d.Action != domain.HookBlock {
		t.Fatalf("the hook must see the tool input on stdin: %+v", d)
	}
	ev := preTool("x")
	ev.ToolInput = map[string]any{"n": 8}
	if d, _ := r.Run(context.Background(), ev); d.Action != domain.HookAllow {
		t.Fatalf("n=8 allows: %+v", d)
	}
}

func TestFailingHookIsAWarnAndOutputIsBounded(t *testing.T) {
	t.Parallel()
	r := &Runner{Snapshot: snap(t, map[string]registry.HookSpec{
		"missing": {Event: domain.HookPreToolUse, Command: "definitely-not-a-command-xyz"},
		"chatty":  {Event: domain.HookPreToolUse, Command: "head -c 200000 /dev/zero | tr '\\0' 'x' >&2; exit 2"},
	}), MaxOutput: 1024}
	d, err := r.Run(context.Background(), preTool("x"))
	if err != nil || d.Action != domain.HookBlock {
		t.Fatalf("a missing binary warns, a chatty block still blocks: %+v %v", d, err)
	}
	if !strings.Contains(d.Reason, "missing: ") || !strings.Contains(d.Reason, "not found") {
		t.Fatalf("the missing binary's sh message is the warning: %q", d.Reason)
	}
	// Per-stream bound (MaxOutput 1024) and the combined bound (2048) both hold.
	if len(d.Reason) > maxReason || !strings.Contains(d.Reason, "missing: ") || len(d.Reason) > 1100 {
		t.Fatalf("the combined reason is bounded (per stream 1024, total %d): %d bytes", maxReason, len(d.Reason))
	}
}

func TestNoRosterIsAnErrorNotAPanic(t *testing.T) {
	t.Parallel()
	var r *Runner
	if d, err := r.Run(context.Background(), preTool("x")); err == nil || d.Action != domain.HookAllow {
		t.Fatalf("a nil runner allows with an error: %+v %v", d, err)
	}
	if _, err := (&Runner{}).Run(context.Background(), preTool("x")); err == nil {
		t.Fatal("a nil snapshot is an error")
	}
	if _, err := os.Stat("/nonexistent"); err == nil {
		t.Fatal("sanity")
	}
}
