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
// kill-switch + analysis model).
type Config struct {
	Enabled bool
	// AnalysisModel optionally names a qualified provider/model for the
	// analysis call ("" = the run's model; still none → typed degrade).
	AnalysisModel string
	// ComposePersona composes the static persona sections (the wrapper
	// fails a run loudly when this errors — no empty prompts).
	ComposePersona func() (string, error)
	// Persist durably records + publishes the intent_analysis event
	// (the daemon wires store.AppendEvent + bus.Publish — the same
	// attempted-durable-first posture as the engine's emits). Nil =
	// the event only rides the run channel.
	Persist func(domain.TimelineEvent)
	// RecallResources supplies the semantic resource hits for the
	// prompt slots (nil = the registry listing).
	RecallResources func(ctx context.Context, goal string) Resources
	// KnownMCPs validates recommended MCP tool names (nil = all drop).
	KnownMCPs func(string) bool
	// OrchestratorModel returns the session's bound ward's model
	// override ("provider/model"), or "" when none/inheriting. Applied
	// BEFORE analysis and the run — the ward's own model orchestrates
	// hosted sessions (the per-ward orchestrator).
	OrchestratorModel func(sessionID string) string
}

// applyOrchestrator lets the session's ward override the run's model
// (explicit config AnalysisModel still wins for the analysis call).
func (w *Wrapper) applyOrchestrator(req *domain.GoalRequest) {
	if w.Cfg.OrchestratorModel == nil || req.SessionID == "" {
		return
	}
	if m := w.Cfg.OrchestratorModel(req.SessionID); m != "" {
		req.Model = m
	}
}

// Wrapper decorates a domain.AgentEngine with the pre-execution intent
// middleware for ROOT goal runs: router → analysis → sanitize against
// the live index → compose the run prompt (static persona + Context +
// Task Analysis + provisioned Skills) → run with the complexity step
// budget. Children (the engine's own delegation), Resume, and chat are
// never analyzed. All analysis failures are non-fatal: the run
// proceeds without analysis, with an event saying why. Every piece of
// per-run state is a RunGoal local — the wrapper itself is stateless.
type Wrapper struct {
	Engine   domain.AgentEngine
	Analyzer *Analyzer
	Snapshot func() registry.Snapshot
	Wards    WardIndex
	RecallFn Recall
	Cfg      Config
}

// RunGoal runs the middleware then the engine, prepending the
// intent_analysis event to the run's stream.
func (w *Wrapper) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	// 0) The ward orchestrates: a bound ward's model override becomes
	// the run's model (analysis target follows unless configured).
	w.applyOrchestrator(&req)
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

	// 2) Analysis (or bypass), fully synchronous: everything the event
	// needs is a local by the time the stream goroutine starts.
	var (
		analysis domain.IntentAnalysis
		drops    []string
		failure  string
		bypassed bool
		budget   int
	)
	switch {
	case !w.Cfg.Enabled:
		bypassed, failure = true, "intent analysis disabled (config)"
	case w.Router().Bypass(req.Goal):
		bypassed, failure = true, "trivial goal (router bypass)"
	default:
		pkt := w.buildPacket(ctx, req.Goal)
		ap, am := w.analysisTarget(req)
		a, err := w.Analyzer.Analyze(ctx, req.Goal, pkt, ap, am)
		if err != nil {
			failure = err.Error() // typed degradation; run proceeds
			break
		}
		a, drops = a.Sanitize(w.knownSkills(), w.knownAgents(), w.knownMCP(), w.knownWards())
		analysis = a
		if b, err := a.Complexity.StepBudget(); err == nil {
			budget = b
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
	events, err := w.Engine.RunGoal(ctx, domain.GoalRequest{
		SessionID:    req.SessionID,
		Goal:         req.Goal,
		Agent:        req.Agent,
		Model:        req.Model,
		Context:      req.Context,
		MaxSteps:     budget,
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		return nil, err
	}
	// 5) Forward the engine's stream, learning the ASSIGNED session id
	// from the run's first event (the engine may assign one), then
	// emit + persist the analysis event. The channel is best-effort
	// (the server may discard it); Persist is the durable path.
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
		ev := w.analysisEvent(sid, analysis, drops, failure, bypassed, budget)
		if w.Cfg.Persist != nil && sid != "" {
			w.Cfg.Persist(ev)
		}
		forward := func(e domain.TimelineEvent) {
			select {
			case out <- e:
			case <-ctx.Done():
			}
		}
		forward(first)
		forward(ev)
		for e := range events {
			forward(e)
		}
	}()
	return out, nil
}

// analysisTarget resolves the analysis call's provider/model: config
// override first, then the run's model ("provider/model"), else empty
// (the completer decides; no model at all → typed degrade).
func (w *Wrapper) analysisTarget(req domain.GoalRequest) (provider, model string) {
	if w.Cfg.AnalysisModel != "" {
		provider, model, _ = strings.Cut(w.Cfg.AnalysisModel, "/")
		return provider, model
	}
	if req.Model != "" {
		p, m, _ := strings.Cut(req.Model, "/")
		if m != "" {
			return p, m
		}
	}
	return "", ""
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
		if s := sysprompt.TaskAnalysis(a); s != "" {
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
		if s := sysprompt.Skills(w.skillBodies(a.RecommendedSkills)); s != "" {
			b.WriteString("\n\n" + s)
		}
	}
	_ = budget
	return b.String()
}

// skillBodies loads the validated recommended skills' bodies from the
// registry (runtime provisioning; no load_skill tool).
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

// analysisEvent builds the intent_analysis Timeline event. Text
// carries the full summary (the SSE surface streams Text; the payload
// rides the persisted record).
func (w *Wrapper) analysisEvent(sessionID string, a domain.IntentAnalysis, drops []string, failure string, bypassed bool, budget int) domain.TimelineEvent {
	payload := &domain.IntentAnalysisPayload{Dropped: drops}
	switch {
	case bypassed:
		payload.Status = domain.IntentStatusBypassed
		payload.Reason = failure
	case a.PrimaryIntent == "" && failure != "":
		payload.Status = domain.IntentStatusFailed
		payload.Reason = failure
	default:
		payload.Status = domain.IntentStatusAnalyzed
		payload.PrimaryIntent = a.PrimaryIntent
		payload.Complexity = a.Complexity
		payload.StepBudget = budget
		payload.Approach = a.ExecutionStrategy.Approach
		payload.WardAction = a.WardRecommendation.Action
		payload.WardName = a.WardRecommendation.WardName
		payload.RecommendedSkills = a.RecommendedSkills
		payload.RecommendedAgents = a.RecommendedAgents
	}
	text := "Pre-execution intent analysis: " + string(payload.Status)
	if payload.Status == domain.IntentStatusAnalyzed {
		text = fmt.Sprintf("Task Analysis: %s — complexity %s (budget %d), approach %s", payload.PrimaryIntent, payload.Complexity, payload.StepBudget, payload.Approach)
		if payload.WardName != "" {
			text += fmt.Sprintf(", ward %s (%s)", payload.WardName, payload.WardAction)
		}
		if len(drops) > 0 {
			text += "; dropped: " + strings.Join(drops, ", ")
		}
	} else if failure != "" {
		text += " — " + failure
	}
	return domain.TimelineEvent{
		ID:             fmt.Sprintf("evt-intent-%d", time.Now().UnixNano()),
		SessionID:      sessionID,
		Kind:           domain.EventIntentAnalysis,
		Timestamp:      time.Now().UTC(),
		Text:           text,
		IntentAnalysis: payload,
	}
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

func (w *Wrapper) knownAgents() func(string) bool {
	return func(n string) bool {
		if n == "root" {
			return true
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

// buildPacket assembles the analyzer's context packet: the FULL host
// roster (registry snapshot: skills/agents/MCPs with descriptions;
// ward vault: names + canonical scope) ranked by the semantic recall
// (the recall's per-kind hit order — host metadata is the authority,
// memory only ORDERS) and capped per kind.
func (w *Wrapper) buildPacket(ctx context.Context, goal string) Packet {
	// Recall order per kind: "name — ..." lines, relevance-ordered.
	rank := map[string]map[string]int{
		"skill": {}, "agent": {}, "mcp": {}, "ward": {},
	}
	if w.Cfg.RecallResources != nil {
		res := w.Cfg.RecallResources(ctx, goal)
		for kind, lines := range map[string][]string{
			"skill": res.Skills, "agent": res.Agents, "mcp": res.MCPs, "ward": res.Wards,
		} {
			for i, line := range lines {
				name, _, _ := strings.Cut(line, " — ")
				name = strings.TrimSpace(name)
				if name != "" {
					if _, dup := rank[kind][name]; !dup {
						rank[kind][name] = i
					}
				}
			}
		}
	}
	rankFn := func(kind, id string) int {
		if r, ok := rank[kind][id]; ok {
			return r
		}
		return 1 << 30 // unrecalled: after every recalled entry
	}

	snap := w.snapshot()
	roster := Packet{}
	for name, s := range snap.Skills {
		roster.Skills = append(roster.Skills, CatalogEntry{ID: name, Description: s.Description})
	}
	for name, a := range snap.Agents {
		roster.Agents = append(roster.Agents, CatalogEntry{ID: name, Description: a.Description})
	}
	for name := range snap.MCPs {
		roster.MCPs = append(roster.MCPs, CatalogEntry{ID: name})
	}
	if w.Wards != nil {
		for _, ward := range w.Wards() {
			roster.Wards = append(roster.Wards, CatalogEntry{ID: ward.Name, Description: ward.Scope})
		}
	}
	return roster.Ranked(rankFn)
}
