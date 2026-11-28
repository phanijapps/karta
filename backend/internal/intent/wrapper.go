package intent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/sysprompt"
)

// WardInfo is one ward for the Existing Wards slot (the daemon adapts
// the vault index: Name + a one-line scope).
type WardInfo struct {
	Name  string
	Scope string
}

// WardIndex supplies the live ward listing (nil = no vault).
type WardIndex func() []WardInfo

// Recall supplies recalled memory and knowledge lines for ## Context
// (the daemon wires chitta; nil = no context section).
type Recall func(ctx context.Context, goal string) (memory, knowledge []string)

// Config is the wrapper's wiring (static-sections composer, the intent
// kill-switch, the model precedence inputs, the accounting seams).
type Config struct {
	Enabled bool
	// AnalysisModel is config.yaml's intent_analysis.model as
	// "provider/model" — kept for compatibility; the Settings card and
	// the agent spec's model both outrank it (analysisTarget).
	AnalysisModel string
	// Settings returns the daemon-managed card settings per run (nil =
	// nothing selected, every default).
	Settings func() Settings
	// Window resolves a model's context window and its source for the
	// decision's limits (nil = no window: no max_tokens, no valve —
	// unit tests).
	Window func(provider, model string) (int, domain.IntentWindowSource)
	// BeginAnalysis opens the intent-analyst's execution row for a run
	// (returning its id; "" = none) and FinishAnalysis closes it with
	// the decision's status and token usage (nil = no row — tests, the
	// Test probe).
	BeginAnalysis  func(sessionID, goal, model string) string
	FinishAnalysis func(execID, status, errMsg string, tokensIn, tokensOut int64)
	// ComposePersona composes the static persona sections (the wrapper
	// fails a run loudly when this errors — no empty prompts).
	ComposePersona func() (string, error)
	// Emit durably records + publishes an analysis event (the daemon
	// wires store.AppendEvent + bus.Publish — the same
	// attempted-durable-first posture as the engine's emits). Nil = the
	// event only rides the run channel.
	Emit func(domain.TimelineEvent)
	// RecallResources supplies the semantic resource hits for the
	// prompt slots (nil = the registry listing).
	RecallResources func(ctx context.Context, goal string) Resources
	// KnownMCPs validates recommended MCP tool names (nil = all drop).
	KnownMCPs func(string) bool
	// OrchestratorModel returns the session's bound ward's model
	// override ("provider/model"), or "" when none/inheriting. Applied
	// BEFORE analysis and the run — the ward's own model orchestrates
	// hosted sessions (the per-ward orchestrator) and, when a ward
	// orchestrates, it wins the analysis model too (operator decision).
	OrchestratorModel func(sessionID string) string
	// TitleSession names an untitled session from its first analysis
	// (the daemon wires store.SetSessionTitle guarded by title == "").
	// Nil = no auto-titling.
	TitleSession func(sessionID, title string)
}

// SlugTitle turns a primary intent into a navigation label: ASCII
// letters/digits as SPACED words, ≤ 64 chars, lower-cased
// ("comparative research on amd vs nvidia").
func SlugTitle(intent string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(intent) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// applyOrchestrator lets the session's ward override the run's model
// and reports that override ("" when the ward inherits).
func (w *Wrapper) applyOrchestrator(req *domain.GoalRequest) string {
	if w.Cfg.OrchestratorModel == nil || req.SessionID == "" {
		return ""
	}
	m := w.Cfg.OrchestratorModel(req.SessionID)
	if m != "" {
		req.Model = m
	}
	return m
}

// Wrapper decorates a domain.AgentEngine with the pre-execution intent
// middleware for ROOT goal runs: router → analysis (the intent-analyst
// agent's decision: execution row, started + terminal events, token
// accounting) → sanitize against the live index → compose the run
// prompt (static persona + Context + Task Analysis + provisioned
// Skills) → run with the complexity step budget. Children (the engine's
// own delegation), Resume, and chat are never analyzed. All analysis
// failures are non-fatal: the run proceeds without analysis, with an
// event saying why. Every piece of per-run state is a RunGoal local —
// the wrapper itself is stateless.
type Wrapper struct {
	Engine   domain.AgentEngine
	Analyzer *Analyzer
	Snapshot func() registry.Snapshot
	Wards    WardIndex
	RecallFn Recall
	Cfg      Config
	// ProgressiveSkills: the engine offers the recommended skills through
	// its `skill` tool (ADK spine, skills.injection: progressive) — the
	// prompt names them instead of inlining their bodies, and the names
	// ride GoalRequest.Skills. False (legacy loop, or skills.injection:
	// bodies) keeps the full SKILL.md bodies in the prompt.
	ProgressiveSkills bool
}

// RunGoal runs the middleware then the engine, prepending the analysis
// events to the run's stream.
func (w *Wrapper) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	// 0) The ward orchestrates: a bound ward's model override becomes
	// the run's model (and wins the analysis target).
	wardModel := w.applyOrchestrator(&req)
	// 1) Static persona (load-critical: a broken composed prompt fails
	// the run loudly — the spec's no-empty-prompt rule).
	var persona string
	if w.Cfg.ComposePersona != nil {
		p, err := w.Cfg.ComposePersona()
		if err != nil {
			return nil, fmt.Errorf("intent: %w", err)
		}
		persona = p
	}

	// 2) Analysis (or bypass), fully synchronous: everything the events
	// need is a local by the time the stream goroutine starts.
	var (
		analysis domain.IntentAnalysis
		drops    []string
		budget   int
		started  *domain.TimelineEvent
		terminal *domain.TimelineEvent
	)
	switch {
	case !w.Cfg.Enabled:
		terminal = w.bypassEvent(req.SessionID, "intent analysis disabled (config)")
	case w.Router().Bypass(req.Goal):
		terminal = w.bypassEvent(req.SessionID, "trivial goal (router bypass)")
	default:
		pkt := w.buildPacket(ctx, req.Goal)
		ap, am := w.analysisTarget(req, wardModel, w.analystSpec())
		lim := w.limits(ap, am)
		execID := w.beginAnalysis(req.SessionID, req.Goal, qualified(ap, am))
		started = w.startedEvent(req.SessionID, execID, ap, am, lim)
		w.emit(*started)
		a := *w.Analyzer // per-run copy: the settings' bounds apply to this decision
		w.settings().ApplyTo(&a)
		t0 := time.Now()
		d, err := a.Analyze(ctx, req.Goal, pkt, ap, am, lim)
		w.finishAnalysis(execID, d, err)
		if err != nil {
			terminal = w.failedEvent(req.SessionID, execID, err.Error(), d, time.Since(t0), ap, am, lim)
			break
		}
		analysis, drops = d.Analysis.Sanitize(w.knownSkills(), w.knownAgents(), w.knownMCP(), w.knownWards())
		if b, err := analysis.Complexity.StepBudget(); err == nil {
			budget = b
		}
		terminal = w.completedEvent(req.SessionID, execID, analysis, drops, d, budget, time.Since(t0), ap, am, lim)
	}
	// A known session gets its trail BEFORE the run (started → terminal
	// → run steps); an ad-hoc run's terminal event waits for the
	// engine-assigned id (below) — its started event has no home.
	if req.SessionID != "" && terminal != nil {
		if terminal.ExecutionID == "" {
			terminal.ExecutionID = "exec-root-" + req.SessionID // the root execution id by convention
		}
		w.emit(*terminal)
	}

	// 2b) Auto-title: an untitled research session takes its name from
	// the first analysis's primary intent (user titles always win —
	// the daemon's TitleSession checks the row).
	if analysis.PrimaryIntent != "" && w.Cfg.TitleSession != nil && req.SessionID != "" {
		if title := SlugTitle(analysis.PrimaryIntent); title != "" {
			w.Cfg.TitleSession(req.SessionID, title)
		}
	}
	// 3) Compose the run's prompt. AGENT-NAMED runs: dynamic sections
	// ONLY (the engine APPENDS them to the agent's body — the agent's
	// identity is never replaced); root runs: persona + dynamic.
	var systemPrompt string
	if req.Agent != "" {
		systemPrompt = w.composePrompt(ctx, req.Goal, "", analysis, budget)
	} else {
		systemPrompt = w.composePrompt(ctx, req.Goal, persona, analysis, budget)
	}

	// 4) Run with the budget + composed prompt.
	runReq := domain.GoalRequest{
		SessionID:    req.SessionID,
		Goal:         req.Goal,
		Agent:        req.Agent,
		Model:        req.Model,
		Context:      req.Context,
		MaxSteps:     budget,
		SystemPrompt: systemPrompt,
	}
	if w.ProgressiveSkills && analysis.PrimaryIntent != "" {
		// The skill tool offers exactly the recommended (and provisioned)
		// skills — decision D3; an analysis with no recommendation offers
		// every provisioned skill (the engine's empty-list rule).
		runReq.Skills = w.provisionedSkills(analysis.RecommendedSkills)
	}
	events, err := w.Engine.RunGoal(ctx, runReq)
	if err != nil {
		return nil, err
	}
	// 5) Forward the engine's stream, learning the ASSIGNED session id
	// from the run's first event (the engine may assign one): an
	// ad-hoc run's terminal event is emitted then. The channel is
	// best-effort (the server may discard it); Emit is the durable path.
	out := make(chan domain.TimelineEvent, 8)
	go func() {
		defer close(out)
		first, ok := <-events
		if !ok {
			return
		}
		sid := first.SessionID
		if sid == "" {
			sid = req.SessionID
		}
		if terminal != nil {
			if req.SessionID == "" && sid != "" {
				terminal.SessionID = sid
				if terminal.ExecutionID == "" {
					terminal.ExecutionID = "exec-root-" + sid // the root execution id by convention
				}
				w.emit(*terminal)
			}
			terminal.SessionID = sid
		}
		forward := func(e domain.TimelineEvent) {
			select {
			case out <- e:
			case <-ctx.Done():
			}
		}
		forward(first)
		if terminal != nil {
			forward(*terminal)
		}
		for e := range events {
			forward(e)
		}
	}()
	return out, nil
}

// analystSpec returns the intent-analyst's registry spec (zero when the
// roster lacks it — the embedded rubric and the chain still work).
func (w *Wrapper) analystSpec() registry.AgentSpec {
	snap := w.snapshot()
	return snap.Agents[AnalystAgentName]
}

// analysisTarget resolves the analysis call's provider/model — ONE
// documented chain: the ward's orchestrator model when a ward
// orchestrates; otherwise the Settings card, the analyst spec's model,
// config.yaml's intent_analysis.model, the run's model, and — for an
// agent-named run whose request names none — that agent's own model
// (the engine resolves the same way; analysis must not fall behind the
// run it precedes). "" = the completer's default.
func (w *Wrapper) analysisTarget(req domain.GoalRequest, wardModel string, spec registry.AgentSpec) (provider, model string) {
	runModel := req.Model
	if runModel == "" && req.Agent != "" {
		runModel = w.snapshot().Agents[req.Agent].Model
	}
	for _, q := range []string{wardModel, w.settings().Qualified(), spec.Model, w.Cfg.AnalysisModel, runModel} {
		if p, m := cutQualified(q); p != "" && m != "" {
			return p, m
		}
	}
	return "", ""
}

func (w *Wrapper) settings() Settings {
	if w.Cfg.Settings == nil {
		return Settings{}
	}
	return w.Cfg.Settings()
}

func (w *Wrapper) limits(provider, model string) Limits {
	if w.Cfg.Window == nil {
		return Limits{}
	}
	window, src := w.Cfg.Window(provider, model)
	return Limits{ContextWindow: window, Source: src}
}

func (w *Wrapper) beginAnalysis(sessionID, goal, model string) string {
	if w.Cfg.BeginAnalysis == nil || sessionID == "" {
		return ""
	}
	return w.Cfg.BeginAnalysis(sessionID, goal, model)
}

// finishAnalysis closes the execution row with the decision's outcome
// and token usage (a zero-usage failure still closes its row).
func (w *Wrapper) finishAnalysis(execID string, d Decision, err error) {
	if w.Cfg.FinishAnalysis == nil || execID == "" {
		return
	}
	status, msg := "completed", ""
	if err != nil {
		status, msg = "failed", err.Error()
	}
	w.Cfg.FinishAnalysis(execID, status, msg, d.TokensIn, d.TokensOut)
}

// Router returns the deterministic router (kept a value method so
// callers can reuse the same rules).
func (w *Wrapper) Router() Router { return Router{} }

// Resume passes straight through: a resumed run was analyzed when it
// started.
func (w *Wrapper) Resume(ctx context.Context, sessionID string, input domain.UserInput) (<-chan domain.TimelineEvent, error) {
	return w.Engine.Resume(ctx, sessionID, input)
}

// Cancel passes through.
func (w *Wrapper) Cancel(ctx context.Context, sessionID string) error {
	return w.Engine.Cancel(ctx, sessionID)
}

// Name keeps the delegate engine's name.
func (w *Wrapper) Name() string { return w.Engine.Name() }

// composePrompt assembles persona + ## Context + ## Task Analysis +
// ## Skills (goal runs only; slices that render empty are omitted).
func (w *Wrapper) composePrompt(ctx context.Context, goal, persona string, a domain.IntentAnalysis, budget int) string {
	var b strings.Builder
	b.WriteString(persona)
	if w.RecallFn != nil {
		mem, kg := w.RecallFn(ctx, goal)
		if s := sysprompt.Context(mem, kg); s != "" {
			b.WriteString("\n\n" + s)
		}
	}
	if a.PrimaryIntent != "" {
		taskAnalysis := sysprompt.TaskAnalysis
		if w.ProgressiveSkills {
			taskAnalysis = sysprompt.TaskAnalysisProgressive
		}
		if s := taskAnalysis(a); s != "" {
			b.WriteString("\n\n" + s)
		}
		// The ward recommendation is ACTIONABLE: instruct the root to
		// call the ward tool as its first structural step. The analysis
		// stays advisory — the MODEL calls the tool; the middleware
		// never enters a ward itself.
		switch a.WardRecommendation.Action {
		case domain.WardUseExisting:
			fmt.Fprintf(&b, "\nWard step: call ward_use(name=%q) to host this run in the recommended ward.\n", a.WardRecommendation.WardName)
		case domain.WardCreateNew:
			arch := a.WardRecommendation.Archetype
			if arch == "" {
				arch = "generic"
			}
			fmt.Fprintf(&b, "\nWard step: call ward_create(name=%q, archetype=%q) to host this run in a new ward.\n", a.WardRecommendation.WardName, arch)
		}
		var skillsSection string
		if w.ProgressiveSkills {
			skillsSection = sysprompt.SkillsProgressive(w.provisionedSkills(a.RecommendedSkills))
		} else {
			skillsSection = sysprompt.Skills(w.skillBodies(a.RecommendedSkills))
		}
		if skillsSection != "" {
			b.WriteString("\n\n" + skillsSection)
		}
	}
	_ = budget
	return b.String()
}

// skillBodies loads the validated recommended skills' bodies from the
// registry (runtime provisioning; no load_skill tool).
// provisionedSkills filters the analysis's skill names to the provisioned
// ones (an unknown recommendation is dropped, like the bodies path does).
func (w *Wrapper) provisionedSkills(names []string) []string {
	snap := w.snapshot()
	out := []string{} // non-nil: "the analysis ran" (an empty list offers every provisioned skill)
	for _, n := range names {
		if _, ok := snap.Skills[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

func (w *Wrapper) skillBodies(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	snap := w.snapshot()
	bodies := map[string]string{}
	for _, n := range names {
		if s, ok := snap.Skills[n]; ok {
			bodies[n] = s.Body
		}
	}
	return bodies
}

func (w *Wrapper) snapshot() registry.Snapshot {
	if w.Snapshot == nil {
		return registry.Snapshot{}
	}
	return w.Snapshot()
}

func (w *Wrapper) knownSkills() func(string) bool {
	return func(n string) bool { _, ok := w.snapshot().Skills[n]; return ok }
}

// knownAgents: root is always valid; system agents (the analyst) are
// never a recommendation.
func (w *Wrapper) knownAgents() func(string) bool {
	return func(n string) bool {
		if n == "root" {
			return true
		}
		if registry.IsSystemAgent(n) {
			return false
		}
		_, ok := w.snapshot().Agents[n]
		return ok
	}
}

func (w *Wrapper) knownWards() func(string) bool {
	if w.Wards == nil {
		return nil
	}
	names := w.Wards()
	set := make(map[string]bool, len(names))
	for _, ward := range names {
		set[ward.Name] = true
	}
	return func(n string) bool { return set[n] }
}

// knownMCP validates recommended MCP tool names: the semantic seam
// when wired, else every recommendation drops (MCP tools are not
// offered at runtime until slice 3).
func (w *Wrapper) knownMCP() func(string) bool {
	if w.Cfg.KnownMCPs != nil {
		return w.Cfg.KnownMCPs
	}
	return func(string) bool { return false }
}
