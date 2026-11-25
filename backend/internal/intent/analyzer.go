package intent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// defaultPrompt is the runtime fallback when the user-owned
// config/prompts/intent-analysis.md is absent or fails to render. Its
// content is kept identical to the seeded file (TestDefaultPromptInSync
// enforces the sync).
//
//go:embed default_prompt.md
var defaultPrompt string

// Completer is the model-call seam (the daemon's llmCaller; a fake in
// tests). One call, plain text in and out.
type Completer interface {
	Complete(ctx context.Context, provider, model, system, user string) (string, error)
}

// ParamCompleter is the optional extended seam: completions carrying
// extra request-body parameters (provider knobs such as z.ai's
// reasoning_effort, the analyst's max_tokens). Implementations that do
// not need it keep the plain Completer.
type ParamCompleter interface {
	Completer
	CompleteWith(ctx context.Context, provider, model, system, user string, extraBody map[string]any) (string, error)
}

// Usage is one completion's REQUEST usage (from the provider's
// response, never estimated from content).
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
}

// UsageCompleter is the optional accounting seam: completions that
// report their usage. Without it the analyzer still runs — tokens read
// as zero (plain fakes in tests).
type UsageCompleter interface {
	CompleteUsage(ctx context.Context, provider, model, system, user string, extra map[string]any) (string, Usage, error)
}

// ErrRequestRejected wraps a provider's 4xx rejection of the request
// body. The analyzer retries the attempt once without max_tokens when a
// full-window request is rejected (a provider whose output cap sits
// below the window refuses it) — the ONE robustness exception to
// "max_tokens = window".
var ErrRequestRejected = errors.New("intent: provider rejected the request")

// ModeTextJSON is the analyst's output mode: plain text carrying one
// JSON object (the provider-mode ladder lands in S4).
const ModeTextJSON = "text_json"

// OutputReserve is the one safety valve's hold-back: a composed prompt
// must fit window − reserve or the packet caps shrink.
const OutputReserve = 4000

// Limits are the decision's model-derived bounds.
type Limits struct {
	// ContextWindow is the model's window (0 = unknown: no max_tokens,
	// no valve — the provider's own defaults apply).
	ContextWindow int
	// Source reports where the window came from (events/settings).
	Source domain.IntentWindowSource
}

// Budget is the prompt's token allowance (window − reserve); ≤0 = no
// valve.
func (l Limits) Budget() int { return l.ContextWindow - OutputReserve }

// Decision is one analysis outcome with its cost — what the terminal
// event and the execution row record.
type Decision struct {
	Analysis domain.IntentAnalysis
	// Attempts counts completions that returned (correction retries
	// included; a rejected request retried without max_tokens is not one).
	Attempts    int
	TokensIn    int64
	TokensOut   int64
	PromptChars int // the composed system rubric + user block (chars)
	// Trimmed: the safety valve fired (packet caps and/or the request
	// echo shrank to fit the budget).
	Trimmed bool
	// MaxTokensDropped: the provider rejected the full-window request
	// and the decision ran without max_tokens.
	MaxTokensDropped bool
	// Packet is the packet the decision actually saw.
	Packet Packet
	// Window is the limits the decision ran under (filled even when the
	// decision fails, so the failure still reports them).
	Window Limits
}

// AnalysisUnavailable is the typed non-fatal degradation: the run
// proceeds without analysis and the reason rides the event.
type AnalysisUnavailable struct {
	Reason string
}

func (e *AnalysisUnavailable) Error() string {
	return "intent: analysis unavailable: " + e.Reason
}

// analysisDeadline bounds the whole decision (all attempts) —
// agentzero's 45s budget: analysis is a classification, never worth an
// unbounded wait. A var, not a const, solely so the deadline test can
// prove the bound without sleeping 45s.
var analysisDeadline = 45 * time.Second

// defaultAttempts is the correction-retry ceiling when no settings say.
const defaultAttempts = 3

// lowEffortReasoning reports whether the target wants the cheap path:
// z.ai glm-5.3* models accept reasoning_effort=low and analysis is not
// a reasoning task.
func lowEffortReasoning(provider, model string) bool {
	return strings.EqualFold(provider, "zai") && strings.HasPrefix(model, "glm-5.3")
}

// Analyzer runs the one intent call: render the prompt with the
// resource slots, complete, strictly parse. Correction retries, then a
// typed degradation — analysis never blocks a run.
type Analyzer struct {
	Completer Completer
	// Snapshot supplies the registry (the analyst's prompt references and
	// the user-owned intent-analysis prompt template).
	Snapshot func() registry.Snapshot
	// Deadline and MaxAttempts bound the decision (0 = the defaults:
	// 45s, 3 attempts) — the daemon wires the settings file.
	Deadline    time.Duration
	MaxAttempts int
	// ReasoningEffort, when set, is sent as reasoning_effort to every
	// model; unset, glm-5.3* gets "low" and other models nothing (their
	// provider default).
	ReasoningEffort string
}

func (a *Analyzer) deadline() time.Duration {
	if a.Deadline > 0 {
		return a.Deadline
	}
	return analysisDeadline
}

func (a *Analyzer) attempts() int {
	if a.MaxAttempts > 0 {
		return a.MaxAttempts
	}
	return defaultAttempts
}

// extraBody builds the provider knobs for the target model: an
// explicitly configured effort goes to every model; otherwise only the
// known-accepting family (z.ai glm-5.3*) gets its default, and other
// models keep their provider default.
func (a *Analyzer) extraBody(provider, model string) map[string]any {
	extra := map[string]any{}
	switch {
	case a.ReasoningEffort != "":
		extra["reasoning_effort"] = a.ReasoningEffort
	case lowEffortReasoning(provider, model):
		extra["reasoning_effort"] = "low"
	}
	return extra
}

// Analyze runs the packet-driven intent decision for a goal under the
// model's limits: the rubric (slots filled from the RANKED packet) is
// the system prompt; the user message embeds the request as escaped
// advisory data; the completion asks for the full window (max_tokens)
// and the prompt is measured against window − reserve (the one safety
// valve — packet caps shrink only then). Failures re-ask with
// CORRECTION FEEDBACK (the code + the fix), at most MaxAttempts inside
// the deadline. Validation runs against the packet's ID sets; the
// wrapper's Sanitize remains the belt-and-braces guard.
func (a *Analyzer) Analyze(ctx context.Context, goal string, pkt Packet, provider, model string, lim Limits) (Decision, error) {
	prompt, user, used, trimmed, err := a.compose(pkt, goal, lim)
	d := Decision{Packet: used, Trimmed: trimmed, PromptChars: len(prompt) + len(user), Window: lim}
	if err != nil {
		return d, err // prompt unusable or window too small: degrade (embedded fallback already tried)
	}
	budget := lim.Budget()
	ctx, cancel := context.WithTimeout(ctx, a.deadline())
	defer cancel()
	extra := a.extraBody(provider, model)
	maxTokens := lim.ContextWindow
	var lastCodes []string
	var lastErr error
	var lastParsed domain.IntentAnalysis
	parsedOnce := false
	for attempt := 0; attempt < a.attempts(); attempt++ {
		msg := user
		if attempt > 0 {
			msg = CorrectionBlock(lastCodes) + "\n\n" + user
		}
		if budget > 0 && estimateTokens(len(prompt)+len(msg)) > budget {
			// Nothing left to shrink (the rubric and the request's floor
			// exceed the window): a typed failure beats an oversized
			// prompt the provider would truncate or refuse.
			return d, &AnalysisUnavailable{Reason: fmt.Sprintf("prompt exceeds the model's window (%d tokens > %d budget) even after trimming", estimateTokens(len(prompt)+len(msg)), budget)}
		}
		body := extra
		if maxTokens > 0 {
			body = withMaxTokens(extra, maxTokens)
		}
		raw, usage, err := a.complete(ctx, provider, model, prompt, msg, body)
		if err != nil {
			if errors.Is(err, ErrRequestRejected) && maxTokens > 0 {
				// The provider's output cap is below the window: the
				// SAME attempt once more without max_tokens.
				d.MaxTokensDropped, maxTokens = true, 0
				attempt--
				continue
			}
			if ctx.Err() != nil {
				return d, &AnalysisUnavailable{Reason: fmt.Sprintf("deadline exceeded (%s)", a.deadline())}
			}
			return d, &AnalysisUnavailable{Reason: "model call failed: " + err.Error()}
		}
		d.Attempts++
		d.TokensIn += usage.PromptTokens
		d.TokensOut += usage.CompletionTokens
		analysis, perr := domain.ParseIntentAnalysis(extractJSON(raw))
		if perr != nil {
			lastCodes = []string{"invalid_json"}
			lastErr = perr
			continue
		}
		if codes := used.Validate(analysis); len(codes) > 0 {
			// Correction retry; remember the parse — if every retry
			// still names unknowns, the LAST PARSED decision returns
			// (the wrapper's Sanitize drops the residue — a good
			// analysis is not wasted on one phantom ID).
			lastParsed, parsedOnce = analysis, true
			lastCodes = codes
			lastErr = fmt.Errorf("unknown recommendations: %s", strings.Join(codes, ", "))
			continue
		}
		d.Analysis = analysis
		return d, nil
	}
	if parsedOnce {
		d.Analysis = lastParsed
		return d, nil
	}
	return d, &AnalysisUnavailable{Reason: fmt.Sprintf("decision failed validation %dx: %v", a.attempts(), lastErr)}
}

// complete routes through the richest seam the completer offers.
func (a *Analyzer) complete(ctx context.Context, provider, model, system, user string, extra map[string]any) (string, Usage, error) {
	if uc, ok := a.Completer.(UsageCompleter); ok {
		return uc.CompleteUsage(ctx, provider, model, system, user, extra)
	}
	if pc, ok := a.Completer.(ParamCompleter); ok && len(extra) > 0 {
		s, err := pc.CompleteWith(ctx, provider, model, system, user, extra)
		return s, Usage{}, err
	}
	s, err := a.Completer.Complete(ctx, provider, model, system, user)
	return s, Usage{}, err
}

// withMaxTokens returns a copy of extra carrying the full-window
// output allowance.
func withMaxTokens(extra map[string]any, window int) map[string]any {
	body := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		body[k] = v
	}
	body["max_tokens"] = window
	return body
}

// compose renders the rubric + request block under the limits. The
// default packet goes first; when the estimate exceeds window −
// reserve, the per-kind caps shrink (ranked heads stay — the packet is
// already ordered) until it fits or the floors are hit, and then the
// request echo gives way. With no window there is no valve.
func (a *Analyzer) compose(pkt Packet, goal string, lim Limits) (prompt, user string, used Packet, trimmed bool, err error) {
	prompt, err = a.renderPacketPrompt(pkt)
	if err != nil {
		return "", "", pkt, false, err
	}
	user = RequestBlock(goal)
	budget := lim.Budget()
	// Correction retries prepend the feedback block: reserve its size so
	// a retry never tips a prompt that fit over the budget.
	correction := 0
	if a.attempts() > 1 {
		correction = len(CorrectionBlock([]string{"unknown_skill:x"}))
	}
	size := func() int { return estimateTokens(len(prompt) + len(user) + correction) }
	if lim.ContextWindow > 0 && budget <= 0 {
		// A known window smaller than the reserve cannot hold any
		// decision: typed failure, never an oversized prompt.
		return "", "", pkt, false, &AnalysisUnavailable{Reason: fmt.Sprintf("model window %d is too small for analysis (reserve %d)", lim.ContextWindow, OutputReserve)}
	}
	if budget <= 0 || size() <= budget {
		return prompt, user, pkt, false, nil
	}
	caps := DefaultCaps()
	for {
		caps = caps.Shrink(size(), budget)
		used = pkt.Capped(caps)
		if prompt, err = a.renderPacketPrompt(used); err != nil {
			return "", "", used, true, err
		}
		if est := size(); est <= budget || caps.AtFloors() {
			if est > budget {
				// The floors are hit and it still does not fit: the
				// request echo gives way (never the rubric) — and when
				// even the rubric alone exceeds the budget that is a
				// typed failure, not a prompt the provider must truncate.
				// Chars-based so rounding can never tip it over: the
				// budget in chars minus the rubric, the correction
				// reserve, and a little slack.
				allow := budget*4 - len(prompt) - correction - 8
				if allow < 256 {
					return "", "", used, true, &AnalysisUnavailable{Reason: fmt.Sprintf("rubric alone (%d tokens) exceeds the prompt budget %d", estimateTokens(len(prompt)), budget)}
				}
				user = RequestBlockLimited(goal, allow)
			}
			return prompt, user, used, true, nil
		}
	}
}

// requiredSlots are the resource-slot markers a user-owned intent
// prompt must carry; a prompt without them cannot receive the runtime
// resource context and is not usable.
var requiredSlots = []string{"{{.Skills}}", "{{.Agents}}", "{{.MCPs}}", "{{.Wards}}"}

// promptData is the template data for the intent prompt.
type promptData struct{ Skills, Agents, MCPs, Wards string }

// renderPacketPrompt renders the rubric with the packet's slot bodies
// (ranked, capped, description-carrying — the host-authoritative
// catalog agentzero's catalog.rs builds).
func (a *Analyzer) renderPacketPrompt(pkt Packet) (string, error) {
	sk, ag, mc, wd := pkt.Render()
	return a.render(promptData{Skills: sk, Agents: ag, MCPs: mc, Wards: wd})
}

// render renders the rubric with the resource slots. The rubric text
// is the analyst agent's own (analyst.go: inline body → its prompt
// reference → intent-analysis → embedded default); a candidate is
// selected only when it parses, carries every required slot, and
// renders successfully; ANY failure falls back to the embedded default
// (a broken user file degrades analysis to the shipped behavior — it
// never blocks the run).
func (a *Analyzer) render(data promptData) (string, error) {
	render := func(text string) (string, error) {
		tmpl, err := template.New("intent").Parse(text)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		if err := tmpl.Execute(&b, data); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	for _, candidate := range a.rubricCandidates() {
		if out, err := render(candidate); err == nil && hasAllSlots(candidate) && strings.TrimSpace(out) != "" {
			return out, nil
		}
		// broken/shape-invalid/empty candidate → next, then default
	}
	out, err := render(defaultPrompt)
	if err != nil {
		return "", &AnalysisUnavailable{Reason: "embedded prompt broken (build bug)"}
	}
	return out, nil
}

// rubricCandidates lists the rubric texts in precedence order (see
// AnalystSpec): the agent's inline body, the prompt it references, the
// intent-analysis prompt.
func (a *Analyzer) rubricCandidates() []string {
	var out []string
	snap := registry.Snapshot{}
	if a.Snapshot != nil {
		snap = a.Snapshot()
	}
	if spec, ok := snap.Agents[AnalystAgentName]; ok {
		if strings.TrimSpace(spec.SystemPrompt) != "" {
			out = append(out, spec.SystemPrompt)
		}
		if spec.Prompt != "" {
			if p, ok := snap.Prompts[spec.Prompt]; ok {
				out = append(out, p.Raw)
			}
		}
	}
	if p, ok := snap.Prompts[intentPromptName]; ok {
		out = append(out, p.Raw)
	}
	return out
}

// hasAllSlots reports whether the template text carries every required
// resource-slot marker.
func hasAllSlots(text string) bool {
	for _, slot := range requiredSlots {
		if !strings.Contains(text, slot) {
			return false
		}
	}
	return true
}

// extractJSON pulls the first complete top-level JSON object out of a
// model response, wherever it sits (prose before/after, code fences,
// same-line fences). Brace matching is string-literal aware so braces
// inside JSON strings do not confuse the scan.
func extractJSON(raw string) string {
	depth, start, inStr, escape := 0, -1, false, false
	for i, r := range raw {
		switch {
		case escape:
			escape = false
		case inStr:
			switch r {
			case '\\':
				escape = true
			case '"':
				inStr = false
			}
		case r == '"':
			inStr = true
		case r == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case r == '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					return raw[start : i+1]
				}
			}
		}
	}
	return strings.TrimSpace(raw)
}
