package chitta

import (
	"context"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
)

func TestDistillationMarkersRoundTrip(t *testing.T) {
	s := newTestSystem(t)
	ctx := context.Background()
	if err := s.MarkDistilled(ctx, "sess-1", 3, true); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDistilled(ctx, "sess-2", 1, false); err != nil {
		t.Fatal(err)
	}
	markers, err := s.DistillationMarkers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 2 {
		t.Fatalf("want 2 markers, got %d", len(markers))
	}
	if m := markers["sess-1"]; !m.DeadLetter || m.Attempts != 3 {
		t.Fatalf("sess-1 marker wrong: %+v", m)
	}
	if m := markers["sess-2"]; m.DeadLetter || m.Attempts != 1 {
		t.Fatalf("sess-2 marker wrong: %+v", m)
	}
}

func TestSeamEventsDistillerOnly(t *testing.T) {
	// Distiller-source seam write emits exactly one event; agent-source
	// seam write emits none (Dispatch owns that path).
	bus := eventbus.New()
	s := &System{store: newFakeDriver(), bus: bus}
	ctx := context.Background()
	distillerHost := domain.ChittaHostContext{Source: domain.SourceDistiller, SessionID: "s-d"}
	agentHostCtx := domain.ChittaHostContext{Source: domain.SourceAgent, SessionID: "s-a"}

	subD := bus.Subscribe(ctx, "s-d")
	if _, err := s.Remember(ctx, []domain.Observation{{Kind: domain.MemoryKindFact, Content: "from distiller"}}, distillerHost); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-subD.Events():
		if ev.ChittaToolInvoked == nil || ev.ChittaToolInvoked.Tool != ToolRemember {
			t.Fatalf("wrong event: %+v", ev)
		}
		if ev.ChittaToolInvoked.Provenance == nil || ev.ChittaToolInvoked.Provenance.Source != domain.SourceDistiller {
			t.Fatalf("provenance must be distiller: %+v", ev.ChittaToolInvoked.Provenance)
		}
	default:
		t.Fatal("distiller seam write must emit one event")
	}
	select {
	case ev := <-subD.Events():
		t.Fatalf("exactly one event expected, got second: %+v", ev)
	default:
	}

	subA := bus.Subscribe(ctx, "s-a")
	if _, err := s.Remember(ctx, []domain.Observation{{Kind: domain.MemoryKindFact, Content: "from agent"}}, agentHostCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-subA.Events():
		t.Fatalf("agent-source seam write must NOT emit (Dispatch owns it): %+v", ev)
	default:
	}
}

// Concurrent close during dispatch/recall: the snapshot-under-mutex fix
// (final-gate P1) must not data-race or nil-panic.
func TestConcurrentCloseDuringUse(t *testing.T) {
	s := newTestSystem(t)
	ctx := context.Background()
	host := domain.ChittaHostContext{Source: domain.SourceAgent}
	// Seed.
	if _, err := s.Remember(ctx, []domain.Observation{{Kind: domain.MemoryKindFact, Content: "race probe"}}, host); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_, _ = s.Recall(ctx, domain.RecallRequest{Query: "race"}, domain.ChittaHostContext{})
			_, _ = s.About(ctx, domain.ExploreRequest{Target: "user"})
		}
	}()
	_ = s.Close() // concurrent with readers
	<-done
	// Post-close reads degrade cleanly.
	if _, err := s.Recall(ctx, domain.RecallRequest{Query: "x"}, domain.ChittaHostContext{}); err == nil {
		t.Fatal("recall after close must degrade")
	}
}
