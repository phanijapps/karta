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
		res := w.resources(ctx, req.Goal)
		ap, am := w.analysisTarget(req)
		a, err := w.Analyzer.Analyze(ctx, req.Goal, res, ap, am)
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

// resources builds the analyzer's resource context: the semantic
// resource index when wired, else the registry listing (cold-index
// fallback).
func (w *Wrapper) resources(ctx context.Context, goal string) Resources {
	if w.Cfg.RecallResources != nil {
		if res := w.Cfg.RecallResources(ctx, goal); len(res.Skills)+len(res.Agents)+len(res.MCPs)+len(res.Wards) > 0 {
			return res
		}
		// empty recall (cold index) → listing fallback below
	}
	snap := w.snapshot()
	res := Resources{}
	for name, s := range snap.Skills {
		res.Skills = append(res.Skills, name+" — "+s.Description)
	}
	for name, a := range snap.Agents {
		if a.Description != "" {
			res.Agents = append(res.Agents, name+" — "+a.Description)
		}
	}
	if w.Wards != nil {
		for _, ward := range w.Wards() {
			line := ward.Name
			if ward.Scope != "" {
				line += " — " + ward.Scope
			}
			res.Wards = append(res.Wards, line)
		}
	}
	return res
}
