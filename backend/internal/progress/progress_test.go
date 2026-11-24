package progress

import (
	"strings"
	"testing"
)

// Productive work: diverse tools, successes → positive score, extends.
func TestProductiveRunExtends(t *testing.T) {
	tr := New()
	for i := 0; i < 12; i++ {
		tr.Record("tool_a", `{"n":1}`, "")
		tr.Record("tool_b", `{"n":2}`, "")
		tr.Record("tool_c", `{"n":3}`, "")
		tr.Tick()
	}
	if tr.Stuck() {
		t.Fatal("diverse successful calls must not be stuck")
	}
	if !tr.ShouldExtend(3) {
		t.Fatal("productive agent deserves extension")
	}
	// Before the soft limit: no nudge.
	if tr.Nudge(20, 25, 50) != "" {
		t.Fatal("no nudge expected before the soft limit")
	}
}

// Stuck loop: same failing call repeated → negative score, stuck, stop.
func TestStuckLoopStops(t *testing.T) {
	tr := New()
	for i := 0; i < 15; i++ {
		tr.Record("read", `{"path":"missing.md"}`, "read: open missing.md: no such file")
	}
	if !tr.Stuck() {
		t.Fatalf("repeated identical failure must be stuck, score=%d", tr.Score())
	}
	nudge := tr.Nudge(10, 0, 50)
	if nudge == "" || !strings.Contains(nudge, "repeating similar actions") {
		t.Fatalf("want stuck nudge, got %q", nudge)
	}
	// Keep failing after the warning → ShouldStop.
	for i := 0; i < 5; i++ {
		tr.Record("read", `{"path":"missing.md"}`, "same error")
	}
	if !tr.ShouldStop() {
		t.Fatalf("stuck + warned + sinking must stop, score=%d", tr.Score())
	}
	if tr.ShouldExtend(3) {
		t.Fatal("stuck agent must not extend")
	}
}

// The failing-call nudge names the tool and carries the error.
func TestFailingCallNudge(t *testing.T) {
	tr := New()
	for i := 0; i < 3; i++ {
		tr.Record("write", `{"path":"x","content":"y"}`, "path escapes the ward")
	}
	nudge := tr.Nudge(5, 0, 50)
	if !strings.Contains(nudge, "write") || !strings.Contains(nudge, "3 times") || !strings.Contains(nudge, "escapes the ward") {
		t.Fatalf("failing-call nudge: %q", nudge)
	}
	// Fires once.
	if again := tr.Nudge(6, 0, 50); again == nudge {
		t.Fatal("same nudge must not fire twice")
	}
}

// Extension resets the window: a fresh start after a grant.
func TestExtensionResets(t *testing.T) {
	tr := New()
	for i := 0; i < 12; i++ {
		tr.Record("tool_a", `{"n":1}`, "some error") // failing but diverse enough not to tank
	}
	tr.Extend()
	if tr.Extensions() != 1 || tr.Score() != 0 || tr.windowToolCalls != 0 {
		t.Fatal("extend must reset the window")
	}
	if tr.stuckWarned {
		t.Fatal("extend must clear the stuck warning")
	}
}

// Soft budget nudge fires at the boundary.
func TestTurnBudgetNudge(t *testing.T) {
	tr := New()
	nudge := tr.Nudge(25, 25, 50)
	if !strings.Contains(nudge, "25 of 50") || !strings.Contains(nudge, "Wrap up") {
		t.Fatalf("turn budget nudge: %q", nudge)
	}
	if again := tr.Nudge(26, 25, 50); again != "" {
		t.Fatal("turn nudge fires once per window")
	}
}

// Planning nudge when no delegations after several calls.
func TestPlanningNudge(t *testing.T) {
	tr := New()
	for i := 0; i < 6; i++ {
		tr.Record("memory_recall", `{"q":"x"}`, "")
	}
	if msg := tr.PlanningNudge(0); !strings.Contains(msg, "delegating") {
		t.Fatalf("planning nudge: %q", msg)
	}
	if msg := tr.PlanningNudge(0); msg != "" {
		t.Fatal("planning nudge fires once")
	}
}
