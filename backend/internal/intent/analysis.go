package intent

// The analysis event trail + the Test probe (intent-analyst-agent S1):
// analysis_started when the call goes out, then exactly one terminal
// event — completed (contract + cost), bypassed, or failed. The wrapper
// emits them through Config.Emit (the daemon's durable-first path) and
// forwards the terminal one on the run's channel.

import (
	"context"
	"fmt"
	"time"

	"zbot/backend/internal/domain"
)

// emit hands one event to the durable seam (a no-op without one or
// without a session to attach it to).
func (w *Wrapper) emit(ev domain.TimelineEvent) {
	if w.Cfg.Emit == nil || ev.SessionID == "" {
		return
	}
	w.Cfg.Emit(ev)
}

func eventID() string { return fmt.Sprintf("evt-intent-%d", time.Now().UnixNano()) }

// startedEvent: the intent-analyst's call is going out under these
// limits.
func (w *Wrapper) startedEvent(sessionID, provider, model string, lim Limits) *domain.TimelineEvent {
	p := &domain.IntentAnalysisPayload{
		Status: domain.IntentStatusStarted, Model: qualified(provider, model), Mode: ModeTextJSON,
		ContextWindow: lim.ContextWindow, WindowSource: lim.Source,
	}
	return &domain.TimelineEvent{
		ID: eventID(), SessionID: sessionID, Kind: domain.EventAnalysisStarted, Timestamp: time.Now().UTC(),
		Text:           fmt.Sprintf("Intent analysis started — %s (window %s)", p.Model, windowLabel(lim)),
		IntentAnalysis: p,
	}
}

// completedEvent: the contract summary plus the decision's cost.
func (w *Wrapper) completedEvent(sessionID string, a domain.IntentAnalysis, drops []string, d Decision, budget int, took time.Duration, provider, model string, lim Limits) *domain.TimelineEvent {
	payload := contractPayload(a, budget)
	payload.Dropped = drops
	fillCost(payload, d, took, provider, model, lim)
	text := fmt.Sprintf("Task Analysis: %s — complexity %s (budget %d), approach %s", payload.PrimaryIntent, payload.Complexity, payload.StepBudget, payload.Approach)
	if payload.WardName != "" {
		text += fmt.Sprintf(", ward %s (%s)", payload.WardName, payload.WardAction)
	}
	if len(drops) > 0 {
		text += "; dropped: " + joinDrops(drops)
	}
	text += fmt.Sprintf(" · %s · %d tokens in / %d out · %d ms · %d attempt(s)", payload.Model, payload.TokensIn, payload.TokensOut, payload.DurationMS, payload.Attempts)
	if payload.Trimmed {
		text += " · packet trimmed to fit the window"
	}
	if d.MaxTokensDropped {
		text += " · provider rejected max_tokens; ran without it"
	}
	return &domain.TimelineEvent{
		ID: eventID(), SessionID: sessionID, Kind: domain.EventAnalysisCompleted, Timestamp: time.Now().UTC(),
		Text: text, IntentAnalysis: payload,
	}
}

// failedEvent: the run proceeds; the reason and the attempts ride along.
func (w *Wrapper) failedEvent(sessionID, reason string, d Decision, took time.Duration, provider, model string, lim Limits) *domain.TimelineEvent {
	payload := &domain.IntentAnalysisPayload{Status: domain.IntentStatusFailed, Reason: reason}
	fillCost(payload, d, took, provider, model, lim)
	return &domain.TimelineEvent{
		ID: eventID(), SessionID: sessionID, Kind: domain.EventAnalysisFailed, Timestamp: time.Now().UTC(),
		Text:           fmt.Sprintf("Pre-execution intent analysis failed — %s (after %d attempt(s))", reason, payload.Attempts),
		IntentAnalysis: payload,
	}
}

// bypassEvent: the router or the kill-switch skipped analysis.
func (w *Wrapper) bypassEvent(sessionID, reason string) *domain.TimelineEvent {
	return &domain.TimelineEvent{
		ID: eventID(), SessionID: sessionID, Kind: domain.EventAnalysisBypassed, Timestamp: time.Now().UTC(),
		Text:           "Pre-execution intent analysis bypassed — " + reason,
		IntentAnalysis: &domain.IntentAnalysisPayload{Status: domain.IntentStatusBypassed, Reason: reason},
	}
}

// contractPayload is the sanitized contract's summary (the session-state
// block and the card render from it).
func contractPayload(a domain.IntentAnalysis, budget int) *domain.IntentAnalysisPayload {
	return &domain.IntentAnalysisPayload{
		Status: domain.IntentStatusAnalyzed, PrimaryIntent: a.PrimaryIntent, Complexity: a.Complexity, StepBudget: budget,
		Approach: a.ExecutionStrategy.Approach, WardAction: a.WardRecommendation.Action, WardName: a.WardRecommendation.WardName,
		RecommendedSkills: a.RecommendedSkills, RecommendedAgents: a.RecommendedAgents,
	}
}

// fillCost sets the decision's model, mode, limits, and cost fields
// (shared by the completed and failed events).
func fillCost(p *domain.IntentAnalysisPayload, d Decision, took time.Duration, provider, model string, lim Limits) {
	p.Model, p.Mode = qualified(provider, model), ModeTextJSON
	p.ContextWindow, p.WindowSource = lim.ContextWindow, lim.Source
	p.TokensIn, p.TokensOut, p.DurationMS, p.Attempts, p.Trimmed = d.TokensIn, d.TokensOut, took.Milliseconds(), d.Attempts, d.Trimmed
}

func windowLabel(lim Limits) string {
	if lim.ContextWindow <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d, %s", lim.ContextWindow, lim.Source)
}

func joinDrops(drops []string) string {
	out := ""
	for i, d := range drops {
		if i > 0 {
			out += ", "
		}
		out += d
	}
	return out
}

func qualified(provider, model string) string {
	switch {
	case provider == "" && model == "":
		return ""
	case provider == "":
		return model
	default:
		return provider + "/" + model
	}
}

// TestResult is one operator probe's outcome (Settings → Advanced →
// Intent Analysis → Test analysis): the real packet and decision with
// its cost — no session, no execution row, no timeline events.
type TestResult struct {
	Status        domain.IntentAnalysisStatus // analyzed | failed
	Reason        string
	Model         string
	ContextWindow int
	WindowSource  domain.IntentWindowSource
	Attempts      int
	TokensIn      int64
	TokensOut     int64
	DurationMS    int64
	Trimmed       bool
	// PacketTokens estimates the composed prompt (rubric + request).
	PacketTokens int
	Analysis     *domain.IntentAnalysisPayload
}

// SampleGoal is the Test probe's default request.
const SampleGoal = "Research the current state of local-first AI agents and write a comparison report"

// TestAnalysis runs one analysis for a goal through the SAME pipeline a
// root run uses (packet, rubric, precedence, limits, bounds) without
// the run — the Settings card's Test analysis.
func (w *Wrapper) TestAnalysis(ctx context.Context, goal string) TestResult {
	if goal == "" {
		goal = SampleGoal
	}
	pkt := w.buildPacket(ctx, goal)
	ap, am := w.analysisTarget(domain.GoalRequest{Goal: goal}, "", w.analystSpec())
	lim := w.limits(ap, am)
	a := *w.Analyzer
	w.settings().ApplyTo(&a)
	t0 := time.Now()
	d, err := a.Analyze(ctx, goal, pkt, ap, am, lim)
	took := time.Since(t0)
	res := TestResult{
		Model: qualified(ap, am), ContextWindow: lim.ContextWindow, WindowSource: lim.Source,
		Attempts: d.Attempts, TokensIn: d.TokensIn, TokensOut: d.TokensOut, DurationMS: took.Milliseconds(),
		Trimmed: d.Trimmed, PacketTokens: estimateTokens(d.PromptChars),
	}
	if err != nil {
		res.Status, res.Reason = domain.IntentStatusFailed, err.Error()
		return res
	}
	res.Status = domain.IntentStatusAnalyzed
	sanitized, _ := d.Analysis.Sanitize(w.knownSkills(), w.knownAgents(), w.knownMCP(), w.knownWards())
	if b, err := sanitized.Complexity.StepBudget(); err == nil {
		res.Analysis = contractPayload(sanitized, b)
	} else {
		res.Analysis = contractPayload(sanitized, 0)
	}
	return res
}

// PacketEstimate sizes the composed prompt for a default packet on the
// sample goal — the Settings derivation's "packet N tokens" (no model
// call; recall runs to rank the packet exactly as a run would).
func (w *Wrapper) PacketEstimate(ctx context.Context) int {
	pkt := w.buildPacket(ctx, SampleGoal)
	prompt, err := w.Analyzer.renderPacketPrompt(pkt)
	if err != nil {
		return 0
	}
	return estimateTokens(len(prompt) + len(RequestBlock(SampleGoal)))
}
