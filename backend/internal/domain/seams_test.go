package domain

import (
	"context"
	"testing"
	"time"
)

// Compile-time proof the fakes satisfy every seam.
var (
	_ AgentEngine  = (*fakeEngine)(nil)
	_ Compactor    = (*fakeCompactor)(nil)
	_ HookRunner   = (*fakeHookRunner)(nil)
	_ EventBus     = (*fakeBus)(nil)
	_ Subscription = (*fakeSubscription)(nil)
	_ Channel      = (*fakeChannel)(nil)
)

type fakeEngine struct{}

func (fakeEngine) Name() string { return "fake" }
func (fakeEngine) RunGoal(ctx context.Context, req GoalRequest) (<-chan TimelineEvent, error) {
	ch := make(chan TimelineEvent, 1)
	ch <- TimelineEvent{ID: "e1", SessionID: req.SessionID, Kind: EventAgentStep, Timestamp: time.Now()}
	return ch, nil
}
func (fakeEngine) Resume(ctx context.Context, sessionID string, input UserInput) (<-chan TimelineEvent, error) {
	return make(chan TimelineEvent), nil
}
func (fakeEngine) Cancel(ctx context.Context, sessionID string) error { return nil }

type fakeCompactor struct{}

func (fakeCompactor) ShouldCompact(ctx context.Context, state ContextState) bool {
	return state.TotalTokens > state.Budget.MaxTotalTokens-state.Budget.ReserveOutput
}
func (fakeCompactor) Compact(ctx context.Context, messages []Message, budget TokenBudget) ([]Message, CompactionStats, error) {
	before := 0
	for _, m := range messages {
		before += m.Tokens
	}
	kept := messages
	if len(kept) > 1 {
		kept = kept[len(kept)/2:]
	}
	after := 0
	for _, m := range kept {
		after += m.Tokens
	}
	return kept, CompactionStats{
		Strategy:       "halve",
		TokensBefore:   before,
		TokensAfter:    after,
		MessagesBefore: len(messages),
		MessagesAfter:  len(kept),
	}, nil
}

type fakeHookRunner struct{ decision HookDecision }

func (f fakeHookRunner) Run(ctx context.Context, event HookEvent) (HookDecision, error) {
	if f.decision.Action == "" {
		return HookDecision{Action: HookAllow}, nil
	}
	return f.decision, nil
}

type fakeSubscription struct{ ch chan TimelineEvent }

func (f *fakeSubscription) Events() <-chan TimelineEvent { return f.ch }
func (f *fakeSubscription) OverflowCount() int64         { return 0 }

type fakeBus struct {
	subs map[string][]*fakeSubscription
}

func (f *fakeBus) Publish(ctx context.Context, event TimelineEvent) error {
	for _, s := range f.subs[event.SessionID] {
		s.ch <- event
	}
	return nil
}
func (f *fakeBus) Subscribe(ctx context.Context, sessionID string) Subscription {
	s := &fakeSubscription{ch: make(chan TimelineEvent, 8)}
	f.subs[sessionID] = append(f.subs[sessionID], s)
	return s
}

type fakeChannel struct{}

func (fakeChannel) Name() string                                  { return "fake" }
func (fakeChannel) Start(ctx context.Context, bus EventBus) error { return nil }
func (fakeChannel) Stop(ctx context.Context) error                { return nil }

func TestSeamsRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"engine runs a goal", func(t *testing.T) {
			e := fakeEngine{}
			ch, err := e.RunGoal(context.Background(), GoalRequest{SessionID: "s1", Goal: "write a poem"})
			if err != nil {
				t.Fatalf("RunGoal: %v", err)
			}
			ev := <-ch
			if ev.SessionID != "s1" || ev.Kind != EventAgentStep {
				t.Fatalf("unexpected event: %+v", ev)
			}
		}},
		{"compactor decision and stats", func(t *testing.T) {
			c := fakeCompactor{}
			msgs := []Message{{Role: RoleUser, Content: "hi", Tokens: 10}, {Role: RoleAssistant, Content: "hello", Tokens: 20}}
			if !c.ShouldCompact(context.Background(), ContextState{TotalTokens: 900, Budget: TokenBudget{MaxTotalTokens: 1000, ReserveOutput: 200}}) {
				t.Fatal("expected ShouldCompact true near budget")
			}
			kept, stats, err := c.Compact(context.Background(), msgs, TokenBudget{MaxTotalTokens: 1000})
			if err != nil {
				t.Fatalf("Compact: %v", err)
			}
			if stats.TokensBefore != 30 || len(kept) != stats.MessagesAfter {
				t.Fatalf("unexpected stats: %+v kept=%d", stats, len(kept))
			}
		}},
		{"hook decisions cover allow block warn", func(t *testing.T) {
			for _, action := range []HookAction{HookAllow, HookBlock, HookWarn} {
				d, err := (fakeHookRunner{decision: HookDecision{Action: action, Reason: "r"}}).Run(context.Background(), HookEvent{Event: HookPreToolUse, ToolName: "write_file"})
				if err != nil || d.Action != action {
					t.Fatalf("hook decision %q: %+v err=%v", action, d, err)
				}
			}
		}},
		{"bus topic delivery", func(t *testing.T) {
			b := &fakeBus{subs: map[string][]*fakeSubscription{}}
			sa := b.Subscribe(context.Background(), "A")
			if err := b.Publish(context.Background(), TimelineEvent{SessionID: "A", Kind: EventAgentStep}); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if (<-sa.Events()).SessionID != "A" {
				t.Fatal("subscriber A did not receive its event")
			}
		}},
		{"compaction triggered event carries stats", func(t *testing.T) {
			ev := TimelineEvent{
				ID: "c1", SessionID: "s1", Kind: EventCompactionTriggered, Timestamp: time.Now(),
				Compaction: &CompactionStats{Strategy: "halve", TokensBefore: 900, TokensAfter: 300, MessagesBefore: 40, MessagesAfter: 12},
			}
			if ev.Compaction.TokensBefore != 900 || ev.Compaction.TokensAfter != 300 || ev.Compaction.Strategy != "halve" {
				t.Fatalf("event stats not representable: %+v", ev)
			}
		}},
		{"channel seam plugs in", func(t *testing.T) {
			var ch Channel = fakeChannel{}
			if ch.Name() != "fake" {
				t.Fatal("channel name mismatch")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
