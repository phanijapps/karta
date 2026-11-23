package intent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// fakeEngine records the last GoalRequest and streams one session event.
type fakeEngine struct {
	lastReq domain.GoalRequest
	events  chan domain.TimelineEvent
}

func (f *fakeEngine) Name() string { return "fake" }
func (f *fakeEngine) RunGoal(_ context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	f.lastReq = req
	if f.events == nil {
		f.events = make(chan domain.TimelineEvent, 1)
		f.events <- domain.TimelineEvent{ID: "e1", SessionID: req.SessionID, Kind: domain.EventSession}
		close(f.events)
	}
	return f.events, nil
}
func (f *fakeEngine) Resume(_ context.Context, _ string, _ domain.UserInput) (<-chan domain.TimelineEvent, error) {
	return nil, errors.New("unused")
}
func (f *fakeEngine) Cancel(_ context.Context, _ string) error { return nil }

func wrapSnap() registry.Snapshot {
	snap := registry.Snapshot{}
	snap.Skills = map[string]registry.SkillSpec{
		"clean-code": {Name: "clean-code", Description: "coding standards", Body: "BODY-OF-CLEAN-CODE"},
		"ghost":      {Name: "ghost", Description: "should drop"}, // recommended but not in index → dropped via name mismatch
	}
	snap.Agents = map[string]registry.AgentSpec{
		"research-agent": {Name: "research-agent", Description: "gathers evidence"},
	}
	return snap
}

func snapFn(s registry.Snapshot) func() registry.Snapshot {
	return func() registry.Snapshot { return s }
}

const wrapperContract = `{"primary_intent":"do-research","hidden_intents":[],"solution_path":"plan then act",
"complexity":"L","recommended_skills":["clean-code","phantom-skill"],
"recommended_agents":["research-agent","root"],"recommended_mcp_tools":[],
"ward_recommendation":{"action":"use_existing","ward_name":"stock-analysis","reason":"domain"},
"execution_strategy":{"approach":"graph","explanation":"multi-agent"}}`

func newWrapper(fc *fakeCompleter, snap registry.Snapshot, wards []WardInfo) *Wrapper {
	eng := &fakeEngine{}
	return &Wrapper{
		Engine:   eng,
		Analyzer: &Analyzer{Completer: fc},
		Snapshot: snapFn(snap),
		Wards:    func() []WardInfo { return wards },
		RecallFn: func(context.Context, string) ([]string, []string) {
			return []string{"prior fact"}, []string{"entity: ACME"}
		},
		Cfg: Config{Enabled: true, ComposePersona: func() (string, error) {
			return "## Role\n\nI am Karta.", nil
		}},
	}
}

func TestWrapperAnalyzedRun(t *testing.T) {
	snap := wrapSnap()
	fc := &fakeCompleter{responses: []string{wrapperContract}}
	w := newWrapper(fc, snap, []WardInfo{{Name: "stock-analysis", Scope: "equity research"}})
	evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s1", Goal: "research amd vs intel"})
	if err != nil {
		t.Fatalf("RunGoal: %v", err)
	}
	var got []domain.TimelineEvent
	for e := range evs {
		got = append(got, e)
	}
	if len(got) != 2 || got[0].Kind != domain.EventIntentAnalysis {
		t.Fatalf("events: %+v", got)
	}
	p := got[0].IntentAnalysis
	if p.Status != domain.IntentStatusAnalyzed || p.Complexity != domain.ComplexityL || p.StepBudget != 16 || p.Approach != domain.ApproachGraph {
		t.Fatalf("payload: %+v", p)
	}
	if p.WardName != "stock-analysis" || p.WardAction != domain.WardUseExisting {
		t.Fatalf("ward: %+v", p)
	}
	if len(p.Dropped) != 1 || p.Dropped[0] != "phantom-skill" {
		t.Fatalf("drops: %+v", p.Dropped)
	}
	// The engine got the budget + the composed prompt.
	eng := w.Engine.(*fakeEngine)
	if eng.lastReq.MaxSteps != 16 {
		t.Fatalf("budget: %d", eng.lastReq.MaxSteps)
	}
	sp := eng.lastReq.SystemPrompt
	for _, want := range []string{"## Role", "I am Karta.", "## Context", "prior fact", "## Task Analysis", "do-research", "## Skills", "BODY-OF-CLEAN-CODE"} {
		if !strings.Contains(sp, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, sp)
		}
	}
	if strings.Contains(sp, "phantom") {
		t.Fatal("dropped skill must not be provisioned")
	}
}

func TestWrapperBypassAndDegrade(t *testing.T) {
	t.Run("trivial goal bypasses analysis", func(t *testing.T) {
		w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "hello"})
		ev := <-evs
		if ev.IntentAnalysis.Status != domain.IntentStatusBypassed {
			t.Fatalf("status: %+v", ev.IntentAnalysis)
		}
		eng := w.Engine.(*fakeEngine)
		if eng.lastReq.MaxSteps != 0 || strings.Contains(eng.lastReq.SystemPrompt, "## Task Analysis") {
			t.Fatalf("bypass must run unbudgeted without analysis sections: %d %.80s", eng.lastReq.MaxSteps, eng.lastReq.SystemPrompt)
		}
	})

	t.Run("analysis failure degrades non-fatally", func(t *testing.T) {
		fc := &fakeCompleter{errs: []error{errors.New("provider down"), errors.New("provider down")}}
		w := newWrapper(fc, wrapSnap(), nil)
		evs, err := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "deep research task"})
		if err != nil {
			t.Fatalf("degraded run must proceed: %v", err)
		}
		ev := <-evs
		if ev.IntentAnalysis.Status != domain.IntentStatusFailed || !strings.Contains(ev.IntentAnalysis.Reason, "provider down") {
			t.Fatalf("payload: %+v", ev.IntentAnalysis)
		}
		eng := w.Engine.(*fakeEngine)
		if eng.lastReq.MaxSteps != 0 {
			t.Fatal("failed analysis must not set a budget")
		}
	})

	t.Run("disabled restores unbudgeted runs", func(t *testing.T) {
		w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
		w.Cfg.Enabled = false
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "anything"})
		ev := <-evs
		if ev.IntentAnalysis.Status != domain.IntentStatusBypassed || ev.IntentAnalysis.Reason == "" {
			t.Fatalf("payload: %+v", ev.IntentAnalysis)
		}
	})
}

func TestWrapperUnknownWardDegrades(t *testing.T) {
	// use_existing for an unlisted ward → create_new in the payload.
	w := newWrapper(&fakeCompleter{responses: []string{wrapperContract}}, wrapSnap(), nil) // no wards
	evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "research task"})
	ev := <-evs
	if ev.IntentAnalysis.WardAction != domain.WardCreateNew {
		t.Fatalf("unknown ward must degrade: %+v", ev.IntentAnalysis)
	}
}

func TestWrapperPersonaFailureFailsLoud(t *testing.T) {
	w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
	w.Cfg.ComposePersona = func() (string, error) {
		return "", errors.New("role missing")
	}
	if _, err := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "task"}); err == nil {
		t.Fatal("broken composed persona must fail the run (no empty prompt)")
	}
}
