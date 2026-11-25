package intent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// cleanContract is wrapperContract without the phantom skill (one
// attempt when the ward exists).
const cleanContract = `{"primary_intent":"do-research","hidden_intents":[],"solution_path":"plan then act",
"complexity":"L","recommended_skills":["clean-code"],
"recommended_agents":["research-agent","root"],"recommended_mcp_tools":[],
"ward_recommendation":{"action":"use_existing","ward_name":"stock-analysis","reason":"domain"},
"execution_strategy":{"approach":"graph","explanation":"multi-agent"}}`

// collectEvents records every emitted event in order.
type eventLog struct {
	mu  sync.Mutex
	evs []domain.TimelineEvent
}

func (l *eventLog) emit(ev domain.TimelineEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evs = append(l.evs, ev)
}

func (l *eventLog) kinds() []domain.TimelineEventKind {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]domain.TimelineEventKind, 0, len(l.evs))
	for _, e := range l.evs {
		out = append(out, e.Kind)
	}
	return out
}

// A known session gets its full trail BEFORE the run: started →
// completed (with tokens, ms, attempts, model, window), each emitted
// once; the terminal event also rides the run's channel.
func TestWrapperEmitsStartedThenTerminal(t *testing.T) {
	log := &eventLog{}
	fc := &fakeCompleter{responses: []string{cleanContract}, usage: []Usage{{PromptTokens: 700, CompletionTokens: 90}}}
	w := newWrapper(fc, wrapSnap(), []WardInfo{{Name: "stock-analysis"}})
	w.Cfg.Emit = log.emit
	w.Cfg.Window = func(p, m string) (int, domain.IntentWindowSource) { return 1048576, domain.WindowOllama }
	w.Cfg.FinishAnalysis = func(id, status, msg string, in, out int64) {}
	var begun string
	w.Cfg.BeginAnalysis = func(sid, goal, model string) string { begun = sid + ":" + model; return "exec-intent-1" }
	evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s1", Goal: "research task", Model: "zai/glm-5.3"})
	if err != nil {
		t.Fatal(err)
	}
	var chanKinds []domain.TimelineEventKind
	for e := range evs {
		chanKinds = append(chanKinds, e.Kind)
	}
	got := log.kinds()
	if len(got) != 2 || got[0] != domain.EventAnalysisStarted || got[1] != domain.EventAnalysisCompleted {
		t.Fatalf("emitted trail: %v", got)
	}
	if begun != "s1:zai/glm-5.3" {
		t.Fatalf("the row opens for the run's model: %q", begun)
	}
	// Both events name the analyst and its execution row.
	for _, ev := range log.evs[:2] {
		if ev.Agent != AnalystAgentName || ev.ExecutionID != "exec-intent-1" {
			t.Fatalf("analyst events must be attributed: %+v", ev)
		}
	}
	start, done := log.evs[0].IntentAnalysis, log.evs[1].IntentAnalysis
	if start.Status != domain.IntentStatusStarted || start.Model != "zai/glm-5.3" || start.ContextWindow != 1048576 || start.WindowSource != domain.WindowOllama || start.Mode != ModeTextJSON {
		t.Fatalf("started: %+v", start)
	}
	if done.Status != domain.IntentStatusAnalyzed || done.TokensIn != 700 || done.TokensOut != 90 || done.Attempts != 1 || done.Trimmed || done.Model != "zai/glm-5.3" || done.WindowSource != domain.WindowOllama || done.ContextWindow != 1048576 {
		t.Fatalf("completed: %+v", done)
	}
	if done.PrimaryIntent != "do-research" || done.StepBudget != 16 || len(done.RecommendedSkills) != 1 {
		t.Fatalf("completed contract: %+v", done)
	}
	if len(chanKinds) != 2 || chanKinds[1] != domain.EventAnalysisCompleted || chanKinds[0] != domain.EventSession {
		t.Fatalf("channel: %v", chanKinds)
	}
	if !strings.Contains(log.evs[1].Text, "700 tokens in / 90 out") || !strings.Contains(log.evs[1].Text, "1 attempt") {
		t.Fatalf("text: %q", log.evs[1].Text)
	}
}

// Bypass and disabled runs emit exactly one bypassed event and never
// open an execution row; a failure emits failed with the reason and
// closes its row as failed.
func TestWrapperBypassEmitsNoRow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goal   string
		on     bool
		reason string
	}{
		{"router bypass", "hello", true, "trivial goal (router bypass)"},
		{"disabled", "research task", false, "intent analysis disabled (config)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &eventLog{}
			w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
			w.Cfg.Enabled = tc.on
			w.Cfg.Emit = log.emit
			w.Cfg.BeginAnalysis = func(sid, goal, model string) string {
				t.Fatal("no execution row for a bypassed run")
				return ""
			}
			w.Cfg.BeginAnalysis = nil
			evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s1", Goal: tc.goal})
			for range evs {
			}
			if got := log.kinds(); len(got) != 1 || got[0] != domain.EventAnalysisBypassed {
				t.Fatalf("trail: %v", got)
			}
			if p := log.evs[0].IntentAnalysis; p.Status != domain.IntentStatusBypassed || p.Reason != tc.reason {
				t.Fatalf("payload: %+v", p)
			}
			// A bypass is the wrapper's call about the ROOT run: root,
			// under the root execution id.
			if ev := log.evs[0]; ev.Agent != domain.AgentRoot || ev.ExecutionID != "exec-root-s1" {
				t.Fatalf("bypass attribution: %+v", ev)
			}
		})
	}
	t.Run("failure closes the row as failed", func(t *testing.T) {
		log := &eventLog{}
		fc := &fakeCompleter{errs: []error{errors.New("provider down"), errors.New("provider down"), errors.New("provider down")}}
		w := newWrapper(fc, wrapSnap(), nil)
		w.Cfg.Emit = log.emit
		var begun string
		var finished []string
		w.Cfg.BeginAnalysis = func(sid, goal, model string) string { begun = sid + ":" + goal; return "exec-intent-1" }
		w.Cfg.FinishAnalysis = func(id, status, msg string, in, out int64) {
			finished = append(finished, id+":"+status+":"+msg)
		}
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s2", Goal: "deep research", Model: "p/m"})
		for range evs {
		}
		if begun != "s2:deep research" {
			t.Fatalf("row opened for the run: %q", begun)
		}
		if len(finished) != 1 || !strings.HasPrefix(finished[0], "exec-intent-1:failed:") || !strings.Contains(finished[0], "provider down") {
			t.Fatalf("row closed: %v", finished)
		}
		got := log.kinds()
		if len(got) != 2 || got[0] != domain.EventAnalysisStarted || got[1] != domain.EventAnalysisFailed {
			t.Fatalf("trail: %v", got)
		}
		if p := log.evs[1].IntentAnalysis; p.Status != domain.IntentStatusFailed || p.Attempts != 0 || !strings.Contains(p.Reason, "provider down") {
			t.Fatalf("failed payload: %+v", p)
		}
	})
}

// The dedicated execution row carries the decision's token usage.
func TestWrapperExecutionRowTokens(t *testing.T) {
	fc := &fakeCompleter{responses: []string{"garbage", wrapperContract}, usage: []Usage{{PromptTokens: 500, CompletionTokens: 50}, {PromptTokens: 520, CompletionTokens: 120}}}
	w := newWrapper(fc, wrapSnap(), nil)
	var rows []string
	w.Cfg.BeginAnalysis = func(sid, goal, model string) string { return "exec-intent-9" }
	w.Cfg.FinishAnalysis = func(id, status, msg string, in, out int64) {
		rows = append(rows, id+":"+status+":"+msg)
		if in != 1020 || out != 170 {
			t.Errorf("tokens rolled into the row: %d/%d", in, out)
		}
	}
	evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s3", Goal: "research", Model: "p/m"})
	for range evs {
	}
	if len(rows) != 1 || rows[0] != "exec-intent-9:completed:" {
		t.Fatalf("rows: %v", rows)
	}
}

// One precedence chain: ward orchestrator → Settings card → agent spec
// → config.yaml → run model.
func TestModelPrecedence(t *testing.T) {
	type tc struct {
		name                       string
		ward, card, spec, cfg, run string
		want                       string
	}
	for _, c := range []tc{
		{"ward wins over everything", "ollama/qwen3", "card/m", "spec/m", "cfg/m", "run/m", "qwen3"},
		{"card wins outside a ward", "", "card/m", "spec/m", "cfg/m", "run/m", "m"},
		{"spec beats config and run", "", "", "spec/m", "cfg/m", "run/m", "m"},
		{"config beats run", "", "", "", "cfg/m", "run/m", "m"},
		{"run model last", "", "", "", "", "prov/runm", "runm"},
		{"nothing → the completer's default", "", "", "", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			fc := &fakeCompleter{responses: []string{wrapperContract}}
			w := newWrapper(fc, wrapSnap(), nil)
			var seenProv, seenModel string
			w.Analyzer = &Analyzer{Completer: &recordingCompleter{inner: fc, onCall: func(p, m string) { seenProv, seenModel = p, m }}}
			w.Cfg.OrchestratorModel = func(string) string { return c.ward }
			cardProvider, cardModel := cutQualified(c.card)
			w.Cfg.Settings = func() Settings { return Settings{Provider: cardProvider, Model: cardModel} }
			w.Cfg.AnalysisModel = c.cfg
			snap := wrapSnap()
			if c.spec != "" {
				snap.Agents[AnalystAgentName] = registry.AgentSpec{Name: AnalystAgentName, Model: c.spec}
			}
			w.Snapshot = snapFn(snap)
			evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s", Goal: "research task", Model: c.run})
			if err != nil {
				t.Fatal(err)
			}
			for range evs {
			}
			if seenModel != c.want || (c.want != "" && seenProv == "") {
				t.Fatalf("analysis target: %s/%s want %s", seenProv, seenModel, c.want)
			}
		})
	}
}

// The Settings card's model is read PER RUN (a save applies without a
// restart).
func TestSettingsReadPerRun(t *testing.T) {
	// No wards → every attempt names an unknown ward, so each run makes
	// its three attempts (the last parsed decision returns).
	fc := &fakeCompleter{responses: []string{wrapperContract, wrapperContract, wrapperContract, wrapperContract, wrapperContract, wrapperContract}}
	w := newWrapper(fc, wrapSnap(), nil)
	current := Settings{}
	w.Cfg.Settings = func() Settings { return current }
	var models []string
	w.Analyzer = &Analyzer{Completer: &recordingCompleter{inner: fc, onCall: func(_, m string) { models = append(models, m) }}}
	for i := 0; i < 2; i++ {
		evs, _ := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s", Goal: "research task", Model: "run/m"})
		for range evs {
		}
		current = Settings{Provider: "card", Model: "picked"}
	}
	if len(models) != 6 || models[0] != "m" || models[5] != "picked" {
		t.Fatalf("card selection must apply on the next run: %v", models)
	}
}

// The Test probe runs the real pipeline (packet, rubric, precedence,
// limits) and reports the decision's cost; no row, no events.
func TestTestAnalysisReportsCost(t *testing.T) {
	fc := &fakeCompleter{responses: []string{cleanContract}, usage: []Usage{{PromptTokens: 800, CompletionTokens: 100}}}
	w := newWrapper(fc, wrapSnap(), []WardInfo{{Name: "stock-analysis"}})
	w.Cfg.Emit = func(domain.TimelineEvent) { t.Fatal("no timeline events from the probe") }
	w.Cfg.BeginAnalysis = func(string, string, string) string { t.Fatal("no execution row from the probe"); return "" }
	w.Cfg.Window = func(p, m string) (int, domain.IntentWindowSource) { return 128000, domain.WindowTable }
	res := w.TestAnalysis(context.Background(), "")
	if res.Status != domain.IntentStatusAnalyzed || res.Model != "" || res.TokensIn != 800 || res.TokensOut != 100 || res.Attempts != 1 {
		t.Fatalf("result: %+v", res)
	}
	if res.ContextWindow != 128000 || res.WindowSource != domain.WindowTable || res.PacketTokens == 0 || res.Trimmed {
		t.Fatalf("limits: %+v", res)
	}
	if res.Analysis == nil || res.Analysis.PrimaryIntent != "do-research" || res.Analysis.StepBudget != 16 {
		t.Fatalf("analysis: %+v", res.Analysis)
	}
	if !strings.Contains(fc.lastUser, SampleGoal) {
		t.Fatal("the sample goal is the default request")
	}
	// A failing probe reports failed + the reason (a model failure
	// degrades at once — no blind retries — so nothing was spent).
	fc2 := &fakeCompleter{errs: []error{errors.New("down")}}
	w2 := newWrapper(fc2, wrapSnap(), nil)
	res = w2.TestAnalysis(context.Background(), "custom goal")
	if res.Status != domain.IntentStatusFailed || !strings.Contains(res.Reason, "down") || res.Attempts != 0 || res.Analysis != nil {
		t.Fatalf("failed probe: %+v", res)
	}
	if !strings.Contains(fc2.lastUser, "custom goal") {
		t.Fatal("a custom goal replaces the sample")
	}
}

// System agents are never catalogued for the analyst.
func TestPacketExcludesSystemAgents(t *testing.T) {
	snap := wrapSnap()
	snap.Agents[AnalystAgentName] = registry.AgentSpec{Name: AnalystAgentName, Description: "the analyst itself"}
	w := newWrapper(&fakeCompleter{}, snap, nil)
	pkt := w.buildPacket(context.Background(), "goal")
	for _, a := range pkt.Agents {
		if a.ID == AnalystAgentName {
			t.Fatal("the analyst must not appear in its own catalog")
		}
	}
	if w.knownAgents()(AnalystAgentName) {
		t.Fatal("the analyst is never a valid recommendation")
	}
	if !w.knownAgents()("research-agent") || !w.knownAgents()("root") {
		t.Fatal("workers and root stay valid")
	}
}

// An agent-named run whose request names no model analyzes on that
// agent's own model — the same resolution the engine applies, so the
// analysis never falls behind the run it precedes.
func TestAnalysisTargetNamedAgentModel(t *testing.T) {
	snap := wrapSnap()
	snap.Agents["research-agent"] = registry.AgentSpec{Name: "research-agent", Description: "gathers evidence", Model: "prov/agent-model"}
	fc := &fakeCompleter{responses: []string{wrapperContract, wrapperContract, wrapperContract}}
	w := newWrapper(fc, snap, []WardInfo{{Name: "stock-analysis"}})
	var seen string
	w.Analyzer = &Analyzer{Completer: &recordingCompleter{inner: fc, onCall: func(_, m string) { seen = m }}}
	evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s", Goal: "research task", Agent: "research-agent"})
	if err != nil {
		t.Fatal(err)
	}
	for range evs {
	}
	if seen != "agent-model" {
		t.Fatalf("analysis must use the named agent's model: %q", seen)
	}
}

// The disabled kill-switch still emits analysis_bypassed AND still lets
// the ward's orchestrator model run the session — the wrapper is
// always installed; only analysis turns off.
func TestDisabledKeepsOrchestratorAndEmitsBypass(t *testing.T) {
	log := &eventLog{}
	w := newWrapper(&fakeCompleter{}, wrapSnap(), nil)
	w.Cfg.Enabled = false
	w.Cfg.Emit = log.emit
	w.Cfg.OrchestratorModel = func(string) string { return "ollama/qwen3" }
	evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-ward", Goal: "research task", Model: "zai/glm"})
	if err != nil {
		t.Fatal(err)
	}
	for range evs {
	}
	if got := log.kinds(); len(got) != 1 || got[0] != domain.EventAnalysisBypassed {
		t.Fatalf("trail: %v", got)
	}
	eng := w.Engine.(*fakeEngine)
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.lastReq.Model != "ollama/qwen3" {
		t.Fatalf("the ward's model must still orchestrate a disabled run: %q", eng.lastReq.Model)
	}
}

// fullContract carries every v1 field the analyst decides.
const fullContract = `{"primary_intent":"inr-usd-pressures","hidden_intents":["compare remittance corridors","flag RBI policy risk"],
"solution_path":"pull the rate series, list the pressure drivers, assess each, write the brief",
"complexity":"M","recommended_skills":["clean-code"],"recommended_agents":["research-agent"],"recommended_mcp_tools":["brave-mcp"],
"ward_recommendation":{"action":"use_existing","ward_name":"stock-analysis","archetype":"research","reason":"currency research belongs here"},
"execution_strategy":{"approach":"graph","explanation":"multi-source evidence needs the research agent"}}`

// The whole decision reaches the surface: the completed event (and the
// Test probe through the same contractPayload) carries hidden intents,
// the solution path, the surviving MCP recommendation, the ward reason,
// and the explanation — not only the summary.
func TestCompletedEventCarriesFullContract(t *testing.T) {
	log := &eventLog{}
	snap := wrapSnap()
	snap.MCPs = map[string]registry.MCPSpec{"brave-mcp": {Command: "brave-mcp"}}
	fc := &fakeCompleter{responses: []string{fullContract, fullContract, fullContract, fullContract}}
	w := newWrapper(fc, snap, []WardInfo{{Name: "stock-analysis"}})
	w.Cfg.Emit = log.emit
	w.Cfg.KnownMCPs = func(n string) bool { return n == "brave-mcp" }
	evs, err := w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-full", Goal: "Analyze INR vs USD pressures", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	for range evs {
	}
	done := log.evs[len(log.evs)-1].IntentAnalysis
	if len(done.HiddenIntents) != 2 || done.HiddenIntents[1] != "flag RBI policy risk" {
		t.Fatalf("hidden intents: %v", done.HiddenIntents)
	}
	if done.SolutionPath == "" || done.Explanation != "multi-source evidence needs the research agent" {
		t.Fatalf("path/explanation: %q %q", done.SolutionPath, done.Explanation)
	}
	if len(done.RecommendedMCPs) != 1 || done.RecommendedMCPs[0] != "brave-mcp" {
		t.Fatalf("mcp recommendations: %v", done.RecommendedMCPs)
	}
	if done.WardReason != "currency research belongs here" || done.WardArchetype != "research" {
		t.Fatalf("ward reason/archetype: %q %q", done.WardReason, done.WardArchetype)
	}
	if log.evs[len(log.evs)-1].IntentAnalysis.Attempts != 1 {
		t.Fatalf("a valid decision takes one attempt: %+v", log.evs[len(log.evs)-1].IntentAnalysis)
	}
	// The probe reports the same decision.
	res := w.TestAnalysis(context.Background(), "")
	if res.Analysis == nil || len(res.Analysis.HiddenIntents) != 2 || res.Analysis.Explanation == "" {
		t.Fatalf("probe: %+v", res.Analysis)
	}
	// An unknown MCP recommendation is dropped (and named) on the event
	// AND on the probe — never silently.
	w.Cfg.KnownMCPs = func(string) bool { return false }
	log.evs = nil
	fc2 := &fakeCompleter{responses: []string{fullContract, fullContract, fullContract, fullContract}}
	w.Analyzer = &Analyzer{Completer: fc2}
	evs, _ = w.RunGoal(context.Background(), domain.GoalRequest{SessionID: "s-full2", Goal: "g", Model: "p/m"})
	for range evs {
	}
	done = log.evs[len(log.evs)-1].IntentAnalysis
	if len(done.RecommendedMCPs) != 0 || len(done.Dropped) != 1 || !strings.Contains(done.Dropped[0], "brave-mcp") {
		t.Fatalf("unknown MCPs must be dropped and named: recs=%v dropped=%v", done.RecommendedMCPs, done.Dropped)
	}
	res = w.TestAnalysis(context.Background(), "")
	if res.Analysis == nil || len(res.Analysis.RecommendedMCPs) != 0 || len(res.Analysis.Dropped) != 1 {
		t.Fatalf("probe must report the drop too: %+v", res.Analysis)
	}
}

// The wire bounds are enforced by clamping: an over-long decision is
// trimmed, never failed.
func TestSanitizeClampsAdvisoryTexts(t *testing.T) {
	a := domain.IntentAnalysis{
		PrimaryIntent: "x", Complexity: domain.ComplexityS,
		HiddenIntents: make([]string, 20), SolutionPath: strings.Repeat("s", 4000),
		WardRecommendation: domain.WardRecommendation{Action: domain.WardCreateNew, WardName: "ww", Reason: strings.Repeat("r", 2000), Archetype: strings.Repeat("a", 100)},
		ExecutionStrategy:  domain.IntentExecutionPlan{Approach: domain.ApproachSimple, Explanation: strings.Repeat("e", 3000)},
	}
	for i := range a.HiddenIntents {
		a.HiddenIntents[i] = strings.Repeat("h", 600)
	}
	a, _ = a.Sanitize(nil, nil, nil, nil)
	if len(a.HiddenIntents) != domain.MaxHiddenIntents || len(a.HiddenIntents[0]) != domain.MaxHiddenIntentLen {
		t.Fatalf("hidden intents clamp: %d / %d", len(a.HiddenIntents), len(a.HiddenIntents[0]))
	}
	if len(a.SolutionPath) != domain.MaxSolutionPathLen || len(a.ExecutionStrategy.Explanation) != domain.MaxExplanationLen ||
		len(a.WardRecommendation.Reason) != domain.MaxWardReasonLen || len(a.WardRecommendation.Archetype) != domain.MaxWardArchetypeLen {
		t.Fatalf("string clamps: %d %d %d %d", len(a.SolutionPath), len(a.ExecutionStrategy.Explanation), len(a.WardRecommendation.Reason), len(a.WardRecommendation.Archetype))
	}
}
