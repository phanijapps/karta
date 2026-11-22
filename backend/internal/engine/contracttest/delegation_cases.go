package contracttest

// Delegation conformance cases (add-engine-delegation): the scenario
// group, the fold-back rule, and the collection helpers. Wired into
// Suite via Fixture.Delegation.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/domain"
)

// DelegationFixture supplies the delegation scenario's controls.
type DelegationFixture struct {
	// StartDelegating starts a ROOT run that delegates once to
	// DelegateAgent and returns the final answer folded back.
	StartDelegating func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error)
	// StartUnknownTarget starts a run that attempts delegation to an
	// UNCONFIGURED agent and must recover (complete after a typed error).
	StartUnknownTarget func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error)
	// StartDepthRefused starts a run whose depth grant is exhausted; its
	// stream must complete with NO delegation events.
	StartDepthRefused func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error)
	// DelegateAgent is the child's agent name (event attribution).
	DelegateAgent string
	// ChildAnswer is the text the child answers with (asserted folded
	// back into the parent's stream).
	ChildAnswer string
	// Engine is the engine under test (for Cancel propagation).
	Engine domain.AgentEngine
}

// CheckFoldBack is the delegation fold-back contract as a pure check:
// the stream must carry delegation events attributed to the child agent
// AND the child's answer somewhere in the parent's stream. Exported so
// engines and meta-tests assert the same rule the suite enforces.
func CheckFoldBack(events []domain.TimelineEvent, delegateAgent, childAnswer string) error {
	sawDelegation, sawAnswer := false, false
	for _, evt := range events {
		if evt.Kind == domain.EventDelegation && evt.Agent == delegateAgent {
			sawDelegation = true
		}
		// The ANSWER must reach the PARENT's stream — a non-delegation
		// event carrying the child's text in its text or tool-result
		// payload (a child delegation echo alone does not prove fold-back).
		if evt.Kind != domain.EventDelegation &&
			(strings.Contains(evt.Text, childAnswer) || (evt.ToolCall != nil && strings.Contains(evt.ToolCall.Output, childAnswer))) {
			sawAnswer = true
		}
	}
	if !sawDelegation {
		return fmt.Errorf("no delegation events attributed to the child agent")
	}
	if !sawAnswer {
		return fmt.Errorf("child answer never folded back into the parent stream")
	}
	return nil
}

// testDelegation pins the delegation contract: child steps surface as
// delegation events, the answer folds back, and cancellation ends the
// run without hanging.
func testDelegation(t *testing.T, fx Fixture) {
	d := fx.Delegation
	t.Run("FoldBack", func(t *testing.T) {
		ch, err := d.StartDelegating(context.Background(), "suite-deleg")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		var events []domain.TimelineEvent
		for evt := range ch {
			events = append(events, evt)
		}
		if err := CheckFoldBack(events, d.DelegateAgent, d.ChildAnswer); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("UnknownTargetRecovers", func(t *testing.T) {
		if d.StartUnknownTarget == nil {
			t.Skip("fixture does not exercise unknown targets")
		}
		ch, err := d.StartUnknownTarget(context.Background(), "suite-deleg-unknown")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		events := drainUntilClosedCollect(t, ch, 5*time.Second)
		sawError, sawCompletion := false, false
		for _, evt := range events {
			// The typed error surfaces in the stream text or the tool
			// result payload — as a PARENT-level event (non-delegation):
			// the parent observed and recovered from the failure.
			if evt.Kind != domain.EventDelegation &&
				(strings.Contains(evt.Text, "unknown agent") || (evt.ToolCall != nil && strings.Contains(evt.ToolCall.Output, "unknown agent"))) {
				sawError = true
			}
			if evt.Kind == domain.EventSession && strings.Contains(evt.Text, "completed") {
				sawCompletion = true
			}
		}
		if !sawError || !sawCompletion {
			t.Fatalf("unknown target must produce a typed error event AND recovery: err=%v done=%v", sawError, sawCompletion)
		}
	})
	t.Run("DepthRefused", func(t *testing.T) {
		if d.StartDepthRefused == nil {
			t.Skip("fixture does not exercise depth refusal")
		}
		ch, err := d.StartDepthRefused(context.Background(), "suite-deleg-depth")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		events := drainUntilClosedCollect(t, ch, 5*time.Second)
		completed := false
		for _, evt := range events {
			if evt.Kind == domain.EventDelegation {
				t.Fatal("depth-refused run must not delegate")
			}
			if evt.Kind == domain.EventSession && strings.Contains(evt.Text, "completed") {
				completed = true
			}
		}
		if !completed {
			t.Fatal("depth-refused run must still COMPLETE (refusal is invisible, not fatal)")
		}
	})
	t.Run("CancelPropagates", func(t *testing.T) {
		ch, err := d.StartDelegating(context.Background(), "suite-deleg-cancel")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := d.Engine.Cancel(context.Background(), "suite-deleg-cancel"); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if !drainUntilClosed(ch, 5*time.Second) {
			t.Fatal("parent stream did not close after cancel (child hung?)")
		}
	})
}

// drainUntilClosedCollect drains into a slice (test helper).
func drainUntilClosedCollect(t *testing.T, ch <-chan domain.TimelineEvent, timeout time.Duration) []domain.TimelineEvent {
	t.Helper()
	var out []domain.TimelineEvent
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, evt)
		case <-deadline:
			t.Fatal("stream did not close in time")
			return out
		}
	}
}

// drainUntilClosed reads until close or timeout (bool: closed in time).
func drainUntilClosed(ch <-chan domain.TimelineEvent, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
