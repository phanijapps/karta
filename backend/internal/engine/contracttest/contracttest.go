// Package contracttest is the reusable conformance suite for the
// domain.AgentEngine seam. It pins the streaming contract every engine
// implementation must honor — event delivery, close-exactly-once on
// done/error/cancel, unknown-session Cancel idempotence, and goroutine
// hygiene — without importing any engine framework.
//
// Usage: an implementation's tests construct a Fixture whose Instance
// provides three deterministic scenarios (quick, blocked, failing) and
// call Suite. The suite runs against fakes here (engine package tests)
// and against the real adapter (engine/eino tests) so semantics stay
// identical across engines.
package contracttest

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"zbot/backend/internal/domain"
)

// Instance is one engine under test with scenario starters. Each starter
// begins a run on the given session and returns the event channel; the
// run's behavior is scenario-specific and deterministic.
type Instance struct {
	Engine domain.AgentEngine

	// StartQuick starts a run that emits >=1 event and completes on its own.
	StartQuick func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error)

	// StartBlocked starts a run that emits >=1 event, then blocks until
	// the returned release func is called (or the run is cancelled).
	StartBlocked func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, func(), error)

	// StartFailing starts a run that fails with an error and closes.
	StartFailing func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error)
}

// Fixture constructs fresh Instances per subtest.
type Fixture struct {
	// New builds a fresh engine + scenarios. Called once per subtest.
	New func(t *testing.T) Instance

	// ResumeImplemented: when false, Resume must return a non-nil error
	// and a nil channel (declared deferral — e.g. the eino adapter today).
	ResumeImplemented bool

	// FailureEventPredicate identifies the event that DESCRIBES a failed
	// run (e.g. kind agent_step mentioning "model error"). Required — a
	// failing run that dies silently violates the no-silent-work rule.
	FailureEventPredicate func(domain.TimelineEvent) bool

	// CompletionEventPredicate identifies the run's COMPLEITION event
	// (e.g. session-kind mentioning "run completed"). Required — the
	// session KIND alone also covers goal/cancel/step-bound transitions,
	// so ordering asserts on it alone can pass without a completion.
	CompletionEventPredicate func(domain.TimelineEvent) bool
}

// Suite runs the full AgentEngine conformance suite.
func Suite(t *testing.T, fx Fixture) {
	t.Run("Name", func(t *testing.T) { testName(t, fx) })
	t.Run("QuickGoal", func(t *testing.T) { testQuickGoal(t, fx) })
	t.Run("FailingGoal", func(t *testing.T) { testFailingGoal(t, fx) })
	t.Run("CloseExactlyOnce", func(t *testing.T) { testCloseExactlyOnce(t, fx) })
	t.Run("CancelRunning", func(t *testing.T) { testCancelRunning(t, fx) })
	t.Run("CancelUnknownSession", func(t *testing.T) { testCancelUnknownSession(t, fx) })
	t.Run("PreCancelledContext", func(t *testing.T) { testPreCancelledContext(t, fx) })
	t.Run("ConcurrentRunsUniqueEventIDs", func(t *testing.T) { testConcurrentEventIDs(t, fx) })
	t.Run("Resume", func(t *testing.T) { testResume(t, fx) })
	t.Run("GoroutineHygiene", func(t *testing.T) { testGoroutineHygiene(t, fx) })
}

func testName(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	if inst.Engine.Name() == "" {
		t.Fatal("Name() returned empty string")
	}
}

// drain reads events until the channel closes; fails on timeout.
func drain(t *testing.T, ch <-chan domain.TimelineEvent, timeout time.Duration) []domain.TimelineEvent {
	t.Helper()
	var events []domain.TimelineEvent
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
		case <-deadline:
			t.Fatalf("drain: channel did not close within %v (got %d events)", timeout, len(events))
			return nil
		}
	}
}

func testQuickGoal(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ch, err := inst.StartQuick(context.Background(), "suite-quick")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	events := drain(t, ch, 5*time.Second)
	if len(events) == 0 {
		t.Fatal("quick goal emitted no events before closing")
	}
	// Ordering contract: the completion transition — matched by the
	// fixture's completion predicate, since the session KIND alone also
	// covers goal/cancel — is the LAST streamed event; nothing may arrive
	// after the run declares itself done.
	if fx.CompletionEventPredicate == nil {
		t.Fatal("Fixture.CompletionEventPredicate is required")
	}
	if last := events[len(events)-1]; !fx.CompletionEventPredicate(last) {
		t.Fatalf("completion event must be last, got kind %s (text %q)", last.Kind, last.Text)
	}
	ids := map[string]bool{}
	for _, evt := range events {
		if evt.SessionID != "suite-quick" {
			t.Fatalf("event %q has session %q, want suite-quick", evt.ID, evt.SessionID)
		}
		// Identity contract: every event carries a non-empty, per-run-unique ID.
		if evt.ID == "" {
			t.Fatalf("event (kind %s) has empty ID", evt.Kind)
		}
		if ids[evt.ID] {
			t.Fatalf("duplicate event id within one run: %q", evt.ID)
		}
		ids[evt.ID] = true
	}
}

func testFailingGoal(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ch, err := inst.StartFailing(context.Background(), "suite-fail")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Silent work is a bug (constitution rule 4): the failure path must
	// DESCRIBE its failure on the stream — matched by the fixture's
	// failure predicate — before closing.
	events := drain(t, ch, 5*time.Second)
	if len(events) == 0 {
		t.Fatal("failing goal closed its channel without emitting any event (silent failure)")
	}
	if fx.FailureEventPredicate == nil {
		t.Fatal("Fixture.FailureEventPredicate is required")
	}
	described := false
	for _, evt := range events {
		if evt.ID == "" {
			t.Fatalf("failure-path event (kind %s) has empty ID", evt.Kind)
		}
		if fx.FailureEventPredicate(evt) {
			described = true
		}
	}
	if !described {
		t.Fatalf("failing run never described its failure: %v", events)
	}
	// Close-exactly-once holds on the error path too.
	assertClosedIdempotent(t, ch)
}

// assertClosedIdempotent verifies reads after close yield the zero value
// immediately and repeatedly (no second event, no block, no panic).
func assertClosedIdempotent(t *testing.T, ch <-chan domain.TimelineEvent) {
	t.Helper()
	for i := 0; i < 3; i++ {
		select {
		case evt, ok := <-ch:
			if ok {
				t.Fatalf("read %d after close returned an event (%q)", i, evt.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("read %d after close blocked", i)
		}
	}
}

func testCloseExactlyOnce(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	// Close-once is exercised on ALL THREE outcomes: done, error, cancel.
	t.Run("done", func(t *testing.T) {
		ch, err := inst.StartQuick(context.Background(), "suite-close-done")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		drain(t, ch, 5*time.Second)
		assertClosedIdempotent(t, ch)
	})
	t.Run("error", func(t *testing.T) {
		ch, err := inst.StartFailing(context.Background(), "suite-close-err")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		drain(t, ch, 5*time.Second)
		assertClosedIdempotent(t, ch)
	})
	t.Run("cancel", func(t *testing.T) {
		ch, release, err := inst.StartBlocked(context.Background(), "suite-close-cancel")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		defer release()
		if err := inst.Engine.Cancel(context.Background(), "suite-close-cancel"); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		drain(t, ch, 5*time.Second)
		assertClosedIdempotent(t, ch)
	})
}

func testCancelRunning(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ch, release, err := inst.StartBlocked(context.Background(), "suite-cancel")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer release() // unblock on every exit path so the run can finish
	if err := inst.Engine.Cancel(context.Background(), "suite-cancel"); err != nil {
		t.Fatalf("cancel running session: %v", err)
	}
	// Cancel must end the run: channel closes in bounded time.
	drain(t, ch, 5*time.Second)
}

func testCancelUnknownSession(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	// Unknown-session Cancel is idempotent: no panic, error tolerated.
	if err := inst.Engine.Cancel(context.Background(), "no-such-session"); err != nil {
		// An error is acceptable ONLY if it names the session; a bare
		// "not found" panic or success-with-side-effects is not.
		if !strings.Contains(err.Error(), "no-such-session") {
			t.Fatalf("unknown-session cancel error should name the session: %v", err)
		}
	}
}

func testPreCancelledContext(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := inst.StartQuick(ctx, "suite-precancel")
	// Either the engine refuses up front (typed error, nil channel) or
	// it starts and closes cleanly — it must never hang.
	if err != nil {
		if ch != nil {
			t.Fatal("error return must come with a nil channel")
		}
		return
	}
	drain(t, ch, 5*time.Second)
}

func testConcurrentEventIDs(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	const runs = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, err := inst.StartQuick(context.Background(), fmt.Sprintf("suite-conc-%d", i))
			if err != nil {
				t.Errorf("run %d start: %v", i, err)
				return
			}
			// Bounded per-run drain: a wedged stream is a bounded failure,
			// never a hang (no assumed upper bound on event count).
			deadline := time.After(5 * time.Second)
			for done := false; !done; {
				select {
				case evt, ok := <-ch:
					if !ok {
						done = true
						break
					}
					mu.Lock()
					if evt.ID != "" && ids[evt.ID] {
						mu.Unlock()
						t.Errorf("duplicate event id across concurrent runs: %q", evt.ID)
						return
					}
					ids[evt.ID] = true
					mu.Unlock()
				case <-deadline:
					t.Errorf("run %d: stream did not close in time", i)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if len(ids) < runs {
		t.Fatalf("expected >=%d distinct events, got %d", runs, len(ids))
	}
}

func testResume(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ctx := context.Background()
	if !fx.ResumeImplemented {
		ch, err := inst.Engine.Resume(ctx, "suite-resume", domain.UserInput{Text: "hi"})
		if err == nil {
			t.Fatal("Resume must return a non-nil error when not implemented")
		}
		if ch != nil {
			t.Fatal("unimplemented Resume must return a nil channel")
		}
		return
	}
	ch, err := inst.Engine.Resume(ctx, "suite-resume", domain.UserInput{Text: "hi"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	drain(t, ch, 5*time.Second)
}

func testGoroutineHygiene(t *testing.T, fx Fixture) {
	inst := fx.New(t)
	ctx := context.Background()
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		ch, err := inst.StartQuick(ctx, "suite-leak")
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		drain(t, ch, 5*time.Second)
	}
	blocked, release, err := inst.StartBlocked(ctx, "suite-leak-blocked")
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	if err := inst.Engine.Cancel(ctx, "suite-leak-blocked"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	drain(t, blocked, 5*time.Second)
	release()

	// Bounded settle: goroutines return to baseline (allow small slack
	// for runtime-internal churn).
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not settle: before=%d now=%d", before, runtime.NumGoroutine())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
