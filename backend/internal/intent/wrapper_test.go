package intent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// fakeEngine records the last GoalRequest and streams one session event.
type fakeEngine struct {
	mu      sync.Mutex
	lastReq domain.GoalRequest
}

func (f *fakeEngine) Name() string { return "fake" }
func (f *fakeEngine) RunGoal(_ context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()
	// Fresh channel per run (a cached closed channel would end every
	// later run instantly — the concurrency test's original failure).
	sid := req.SessionID
	if sid == "" {
		sid = "s1" // the real engine assigns one; the wrapper must learn it from this event
	}
	events := make(chan domain.TimelineEvent, 1)
	events <- domain.TimelineEvent{ID: "e1", SessionID: sid, Kind: domain.EventSession}
	close(events)
	return events, nil
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
	if len(got) != 2 || got[0].Kind != domain.EventSession || got[1].Kind != domain.EventAnalysisCompleted {
		t.Fatalf("events: %+v", got) // session first (the engine assigns it), analysis second
	}
	p := got[1].IntentAnalysis
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
	eng.mu.Lock()
	if eng.lastReq.MaxSteps != 16 {
		eng.mu.Unlock()
		t.Fatalf("budget: %d", eng.lastReq.MaxSteps)
	}
	sp := eng.lastReq.SystemPrompt
	eng.mu.Unlock()
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
		<-evs // session event
		ev := <-evs
		if ev.IntentAnalysis.Status != domain.IntentStatusBypassed {
			t.Fatalf("status: %+v", ev.IntentAnalysis)
		}
		eng := w.Engine.(*fakeEngine)
		eng.mu.Lock()
		defer eng.mu.Unlock()
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
		<-evs // session event
		ev := <-evs
		if ev.IntentAnalysis.Status != domain.IntentStatusFailed || !strings.Contains(ev.IntentAnalysis.Reason, "provider down") {
			t.Fatalf("payload: %+v", ev.IntentAnalysis)
		}
		eng := w.Engine.(*fakeEngine)
		eng.mu.Lock()
		defer eng.mu.Unlock()
		if eng.lastReq.MaxSteps != 0 {
			t.Fatal("failed analysis must not set a budget")
		}
	})

	t.Run("disabled restores unbudgeted runs", func(t *testing.T) {
		w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
		w.Cfg.Enabled = false
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{Goal: "anything"})
		<-evs // session event
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
	<-evs // session event
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

// Section ORDER is asserted (not just presence), the analysis event is
// PERSISTED once with the assigned session id, and concurrent runs
// stay independent (phase-3 gate F10 + F1).
func TestWrapperOrderPersistenceConcurrency(t *testing.T) {
	t.Run("sections in order and persist called once", func(t *testing.T) {
		snap := wrapSnap()
		fc := &fakeCompleter{responses: []string{wrapperContract}}
		w := newWrapper(fc, snap, []WardInfo{{Name: "stock-analysis", Scope: "equity"}})
		var persisted []domain.TimelineEvent
		w.Cfg.Emit = func(ev domain.TimelineEvent) { persisted = append(persisted, ev) }
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "", Goal: "research task"})
		for range evs { // drain
		}
		// An ad-hoc run (no session id) emits only its terminal event,
		// once the engine's first event reveals the id; the started
		// event has no home to attach to.
		if len(persisted) != 1 || persisted[0].Kind != domain.EventAnalysisCompleted {
			t.Fatalf("persist: %+v", persisted)
		}
		// Session id learned from the engine's first event (fake assigns s1).
		if persisted[0].SessionID != "s1" {
			t.Fatalf("persisted session: %q", persisted[0].SessionID)
		}
		sp := func() string {
			e := w.Engine.(*fakeEngine)
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.lastReq.SystemPrompt
		}()
		order := []int{strings.Index(sp, "## Role"), strings.Index(sp, "## Context"), strings.Index(sp, "## Task Analysis"), strings.Index(sp, "## Skills")}
		for i := 1; i < len(order); i++ {
			if order[i-1] >= order[i] || order[i] < 0 {
				t.Fatalf("section order wrong: %v in:\n%s", order, sp)
			}
		}
	})

	t.Run("concurrent runs are independent", func(t *testing.T) {
		snap := wrapSnap()
		w := newWrapper(&fakeCompleter{responses: []string{wrapperContract, "bad", "bad"}}, snap, nil)
		var mu sync.Mutex
		var persisted int
		w.Cfg.Emit = func(ev domain.TimelineEvent) {
			if ev.Kind == domain.EventAnalysisCompleted || ev.Kind == domain.EventAnalysisFailed {
				mu.Lock()
				persisted++
				mu.Unlock()
			}
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: fmt.Sprintf("s%d", i), Goal: "research task"})
				if err != nil {
					t.Error(err)
					return
				}
				for range evs {
				}
			}(i)
		}
		wg.Wait()
		if persisted != 4 {
			t.Fatalf("persisted %d terminal events, want 4 (one analyzed, three failed on the scripted bad output)", persisted)
		}
	})

	t.Run("agent-named run composes dynamic-only prompt", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{wrapperContract}}
		w := newWrapper(fc, wrapSnap(), []WardInfo{{Name: "stock-analysis"}})
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s1", Goal: "research task", Agent: "research-agent"})
		for range evs {
		}
		sp := func() string {
			e := w.Engine.(*fakeEngine)
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.lastReq.SystemPrompt
		}()
		if strings.Contains(sp, "## Role") {
			t.Fatal("agent run must not carry the root persona")
		}
		for _, want := range []string{"## Context", "## Task Analysis", "## Skills"} {
			if !strings.Contains(sp, want) {
				t.Fatalf("agent run missing %q:\n%s", want, sp)
			}
		}
	})
}

// TestWrapperOrchestratorOverride (per-ward orchestrator): a bound
// ward's model override becomes the run's model before analysis and
// execution; no override keeps the picker default.
func TestWrapperOrchestratorOverride(t *testing.T) {
	fc := &fakeCompleter{responses: []string{wrapperContract}}
	w := newWrapper(fc, wrapSnap(), nil)
	var analysisModel string
	w.Analyzer = &Analyzer{Completer: &recordingCompleter{inner: fc, onCall: func(_, model string) { analysisModel = model }}}
	w.Cfg.OrchestratorModel = func(sessionID string) string {
		if sessionID == "s-ward" {
			return "ollama/qwen3"
		}
		return ""
	}
	eng := w.Engine.(*fakeEngine)
	_, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-ward", Goal: "g", Model: "zai/glm-5.3-flash"})
	if err != nil {
		t.Fatal(err)
	}
	eng.mu.Lock()
	got := eng.lastReq.Model
	eng.mu.Unlock()
	if got != "ollama/qwen3" {
		t.Fatalf("ward override must win: %q", got)
	}
	if analysisModel != "qwen3" {
		t.Fatalf("analysis must follow the ward's model: %q", analysisModel)
	}
	// No override: the picker default stands.
	eng.mu.Lock()
	eng.lastReq.Model = ""
	eng.mu.Unlock()
	if _, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-plain", Goal: "g", Model: "zai/glm-5.3-flash"}); err != nil {
		t.Fatal(err)
	}
	eng.mu.Lock()
	got = eng.lastReq.Model
	eng.mu.Unlock()
	if got != "zai/glm-5.3-flash" {
		t.Fatalf("no override must inherit: %q", got)
	}
}

// recordingCompleter captures the analysis model target.
type recordingCompleter struct {
	inner  Completer
	onCall func(provider, model string)
}

func (r *recordingCompleter) Complete(ctx context.Context, provider, model, system, user string) (string, error) {
	r.onCall(provider, model)
	return r.inner.Complete(ctx, provider, model, system, user)
}

// TestAutoTitleFromAnalysis (harden-mcp-and-ux): an untitled research
// session is named from the first analysis's primary intent; the
// hook fires with a slug; SlugTitle bounds and sanitizes.
func TestAutoTitleFromAnalysis(t *testing.T) {
	fc := &fakeCompleter{responses: []string{wrapperContract}}
	w := newWrapper(fc, wrapSnap(), nil)
	var titled []string
	w.Cfg.TitleSession = func(sessionID, title string) { titled = append(titled, sessionID+"="+title) }
	if _, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-untitled", Goal: "g", Model: "p/m"}); err != nil {
		t.Fatal(err)
	}
	if len(titled) != 1 || titled[0] != "s-untitled=do research" {
		t.Fatalf("auto-title: %v", titled)
	}
	for _, tc := range []struct{ in, want string }{
		{"Comparative research: AMD vs NVIDIA (datacenter) — 2026!", "comparative research amd vs nvidia datacenter 2026"},
		{"Émigré café", "migr caf"},
		{"", ""},
		{strings.Repeat("x", 100), strings.Repeat("x", 64)},
	} {
		if got := SlugTitle(tc.in); got != tc.want {
			t.Fatalf("SlugTitle(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// Progressive skills (engine-adk-spine D3): on a spine run the prompt
// NAMES the recommended, provisioned skills instead of inlining their
// bodies, points the model at the skill tool, and passes the names to the
// engine (GoalRequest.Skills). The bodies path is untouched (legacy loop,
// or skills.injection: bodies).
func TestWrapperProgressiveSkills(t *testing.T) {
	snap := wrapSnap()
	fc := &fakeCompleter{responses: []string{wrapperContract}}
	w := newWrapper(fc, snap, nil)
	w.ProgressiveSkills = true
	if _, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s1", Goal: "research amd vs intel"}); err != nil {
		t.Fatalf("RunGoal: %v", err)
	}
	eng := w.Engine.(*fakeEngine)
	eng.mu.Lock()
	req := eng.lastReq
	eng.mu.Unlock()
	sp := req.SystemPrompt
	for _, want := range []string{"## Skills", "clean-code", "skill(name)", "Skills one call away"} {
		if !strings.Contains(sp, want) {
			t.Fatalf("progressive prompt missing %q:\n%s", want, sp)
		}
	}
	if strings.Contains(sp, "BODY-OF-CLEAN-CODE") || strings.Contains(sp, "Skills provided in context") {
		t.Fatalf("progressive prompt must not inline bodies or claim they are provided:\n%s", sp)
	}
	if len(req.Skills) != 1 || req.Skills[0] != "clean-code" {
		t.Fatalf("the engine must receive the provisioned recommendations: %v", req.Skills)
	}
}
