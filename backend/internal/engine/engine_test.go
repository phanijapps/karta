package engine_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine"
	"zbot/backend/internal/engine/contracttest"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/tools"
)

// fakeEngine is a minimal domain.AgentEngine used to run the conformance
// suite inside the seam package itself: it proves the suite (and therefore
// the seam's documented contract) is satisfiable without any framework.
type fakeEngine struct {
	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
	failNext bool
	idSeq    int64
}

func newFakeEngine() *fakeEngine { return &fakeEngine{cancels: map[string]context.CancelFunc{}} }

func (f *fakeEngine) Name() string { return "fake" }

// nextEventID mints a per-EVENT id (never reused) — Timeline Tree identity
// demands one id per event (this is the uniqueness the conformance suite
// pins and the class of F-22).
func (f *fakeEngine) nextEventID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idSeq++
	return fmt.Sprintf("fake-evt-%d", f.idSeq)
}

func (f *fakeEngine) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("goal-%d", len(f.cancels)+1)
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	f.cancels[sessionID] = cancel
	f.mu.Unlock()

	events := make(chan domain.TimelineEvent, 8)
	go func() {
		defer func() {
			cancel()
			f.mu.Lock()
			delete(f.cancels, sessionID)
			f.mu.Unlock()
			close(events)
		}()
		emit := func(kind domain.TimelineEventKind, text string) {
			evt := domain.TimelineEvent{ID: f.nextEventID(), SessionID: sessionID, Kind: kind, Text: text}
			select {
			case events <- evt:
			case <-runCtx.Done():
			}
		}
		emit(domain.EventSession, "goal: "+req.Goal)
		f.mu.Lock()
		fail := f.failNext
		f.failNext = false
		f.mu.Unlock()
		switch {
		case fail || req.Goal == "fail":
			emit(domain.EventAgentStep, "engine: model error: forced failure")
			return // error path: close without completing
		case req.Goal == "quick":
			emit(domain.EventSession, "run completed")
			return
		}
		<-runCtx.Done() // blocked: run until cancelled (engine Cancel or caller ctx)
		emit(domain.EventSession, "run completed")
	}()
	return events, nil
}

func (f *fakeEngine) Cancel(_ context.Context, sessionID string) error {
	f.mu.Lock()
	cancel, ok := f.cancels[sessionID]
	f.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

func (f *fakeEngine) Resume(_ context.Context, _ string, _ domain.UserInput) (<-chan domain.TimelineEvent, error) {
	return nil, fmt.Errorf("engine: resume not yet implemented")
}

// TestContractSuite runs the shared conformance suite against the fake.
func TestContractSuite(t *testing.T) {
	contracttest.Suite(t, contracttest.Fixture{
		New: func(t *testing.T) contracttest.Instance {
			fe := newFakeEngine()
			return contracttest.Instance{
				Engine: fe,
				StartQuick: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
					return fe.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "quick"})
				},
				StartBlocked: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, func(), error) {
					ch, err := fe.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "blocked"})
					// The fake blocks on its own run ctx; release = cancel via engine Cancel.
					return ch, func() {}, err
				},
				StartFailing: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
					return fe.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "fail"})
				},
			}
		},
		ResumeImplemented: false,
		FailureEventPredicate: func(evt domain.TimelineEvent) bool {
			return evt.Kind == domain.EventAgentStep && strings.Contains(evt.Text, "model error")
		},
		CompletionEventPredicate: func(evt domain.TimelineEvent) bool {
			return evt.Kind == domain.EventSession && strings.Contains(evt.Text, "run completed")
		},
	})
}

// --- ResolveAgent ---------------------------------------------------------

func TestResolveAgentDefault(t *testing.T) {
	def, err := engine.ResolveAgent(registry.Snapshot{}, "")
	if err != nil {
		t.Fatalf("empty name: %v", err)
	}
	// Pin the CONTRACT, never the literal prompt text (F-18 is a recorded
	// rule conflict; asserting the literal would enshrine it).
	if def.Name == "" || def.SystemPrompt == "" {
		t.Fatalf("default agent must have a name and non-empty SystemPrompt, got %+v", def)
	}
}

func TestResolveAgentUnknown(t *testing.T) {
	_, err := engine.ResolveAgent(registry.Snapshot{Agents: map[string]registry.AgentSpec{"helper": {}}}, "ghost")
	if err == nil {
		t.Fatal("unknown agent must error")
	}
	// ResolveAgent returns fmt.Errorf — assert message content, not a type.
	for _, want := range []string{"unknown agent", `"ghost"`, "helper"} {
		if !contains(err.Error(), want) {
			t.Fatalf("error %q missing %q (should list the name and configured agents)", err.Error(), want)
		}
	}
}

func TestResolveAgentKnown(t *testing.T) {
	snap := registry.Snapshot{Agents: map[string]registry.AgentSpec{
		"helper": {Name: "helper", SystemPrompt: "be helpful", Model: "prov/m1", Tools: []string{"clock_now"}},
	}}
	def, err := engine.ResolveAgent(snap, "helper")
	if err != nil {
		t.Fatalf("known agent: %v", err)
	}
	if def.Name != "helper" || def.SystemPrompt != "be helpful" || def.Model != "prov/m1" || len(def.ToolNames) != 1 {
		t.Fatalf("resolved def mismatch: %+v", def)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- FilterTools / ExecuteTool ---------------------------------------------

type stubTool struct{ name string }

func (s stubTool) Def() tools.Def { return tools.Def{Name: s.name, Description: "stub"} }
func (s stubTool) Exec(_ context.Context, _ string) tools.Result {
	return tools.Result{Output: "ran " + s.name}
}

type stubProvider struct{ list []tools.Tool }

func (p stubProvider) Tools() []tools.Tool { return p.list }

func TestFilterTools(t *testing.T) {
	prov := stubProvider{list: []tools.Tool{stubTool{"a"}, stubTool{"b"}, stubTool{"c"}}}
	if got := engine.FilterTools(prov, nil); len(got) != 3 {
		t.Fatalf("empty allowlist = all tools, got %d", len(got))
	}
	got := engine.FilterTools(prov, []string{"a", "c", "zz"})
	if len(got) != 2 || got[0].Def().Name != "a" || got[1].Def().Name != "c" {
		t.Fatalf("allowlist filter mismatch: %v", got)
	}
	if got := engine.FilterTools(prov, []string{"zz"}); len(got) != 0 {
		t.Fatalf("unknown allowlist should filter everything, got %v", got)
	}
}

func TestExecuteTool(t *testing.T) {
	prov := stubProvider{list: []tools.Tool{stubTool{"a"}}}
	res := engine.ExecuteTool(context.Background(), prov, "a", `{}`)
	if res.Err != nil || res.Output != "ran a" {
		t.Fatalf("execute known tool: %+v", res)
	}
	// Unknown tools return a typed error RESULT the model can see (not a panic).
	res = engine.ExecuteTool(context.Background(), prov, "nope", `{}`)
	if res.Err == nil {
		t.Fatal("unknown tool must return an error result")
	}
}
