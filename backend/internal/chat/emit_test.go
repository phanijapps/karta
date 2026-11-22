package chat

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// These tests PIN the emit paths' current failure semantics (finding
// F-15). The two sites behave OPPOSITELY on persistence failure — the
// chat SKIPS publish when AppendEvent fails; the eino adapter ignores
// the append error and publishes anyway (see engine/eino tests). If
// either changes silently, these tests fail. The follow-up change
// (backend-engine-run-ownership) owns the policy decision
// (retry/log/publish-anywhere), not this change.

func newEmitService(t *testing.T, db *store.Store, bus *eventbus.Bus) *Service {
	t.Helper()
	var seq atomic.Int64
	return &Service{
		store: db,
		bus:   bus,
		idGen: func() string { return "evt-" + string(rune('a'+seq.Add(1))) }, // distinct IDs
	}
}

// drainEvents collects bus events until quiet or timeout.
func drainEvents(t *testing.T, ch <-chan domain.TimelineEvent, quiet time.Duration) []domain.TimelineEvent {
	t.Helper()
	var out []domain.TimelineEvent
	quietTimer := time.NewTimer(quiet)
	defer quietTimer.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, evt)
			quietTimer.Reset(quiet)
		case <-quietTimer.C:
			return out
		}
	}
}

// happyPath: both emit methods persist AND publish.
func TestEmitPersistsAndPublishes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "emit.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := eventbus.New()
	sub := bus.Subscribe(context.Background(), SessionID)
	svc := newEmitService(t, db, bus)

	svc.emitEvent(store.Message{ID: "m1", Role: "user", Content: "hi", Seq: 1}, "exec1", "")
	svc.emitToolEvent("exec1", "clock_now", tools.Result{Output: "now"})

	persisted, err := db.Events(SessionID, "", 16)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(persisted))
	}
	published := drainEvents(t, sub.Events(), 150*time.Millisecond)
	if len(published) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(published))
	}
}

// F-15 chat-side semantics, per method: a FAILED AppendEvent means NO
// volatile publish — subscribers never see an unpersisted event.
func TestEmitSkipsPublishWhenPersistFails(t *testing.T) {
	newCase := func(name string, emit func(svc *Service)) {
		t.Run(name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "emit.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			bus := eventbus.New()
			sub := bus.Subscribe(context.Background(), SessionID)
			svc := newEmitService(t, db, bus)

			// Persist failure: close the DB so AppendEvent errors.
			if err := db.Close(); err != nil {
				t.Fatalf("close store: %v", err)
			}
			emit(svc)

			if got := drainEvents(t, sub.Events(), 150*time.Millisecond); len(got) != 0 {
				t.Fatalf("event published despite AppendEvent failure — chat-side semantics changed: %v", got)
			}
		})
	}
	newCase("emitEvent", func(svc *Service) {
		svc.emitEvent(store.Message{ID: "m1", Role: "user", Content: "hi", Seq: 1}, "exec1", "")
	})
	newCase("emitToolEvent", func(svc *Service) {
		svc.emitToolEvent("exec1", "clock_now", tools.Result{Output: "now"})
	})
}

// F-15 chat-side semantics: a closed bus (Publish → ErrClosed) is
// swallowed — the emit path must not panic, and (open store) the events
// still persist: the swallow never costs durability.
func TestEmitSwallowsClosedBus(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "emit.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := eventbus.New()
	bus.Close() // Publish hereafter returns ErrClosed.
	svc := newEmitService(t, db, bus)

	// Must not panic; the swallow is the pinned (documented) behavior.
	svc.emitEvent(store.Message{ID: "m1", Role: "user", Content: "hi", Seq: 1}, "exec1", "")
	svc.emitToolEvent("exec1", "clock_now", tools.Result{Output: "now"})

	persisted, err := db.Events(SessionID, "", 16)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("expected both events persisted despite closed bus, got %d", len(persisted))
	}
}
