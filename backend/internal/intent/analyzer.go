package intent

import (
	"context"
	_ "embed"
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
// reasoning_effort). Implementations that do not need it keep the
// plain Completer.
type ParamCompleter interface {
	Completer
	CompleteWith(ctx context.Context, provider, model, system, user string, extraBody map[string]any) (string, error)
}

// analysisDeadline bounds the whole decision (all attempts) —
// agentzero's 45s budget: analysis is a classification, never worth an
// unbounded wait. A var, not a const, solely so the deadline test can
// prove the bound without sleeping 45s.
var analysisDeadline = 45 * time.Second

// lowEffortReasoning reports whether the target wants the cheap path:
// z.ai glm-5.3* models accept reasoning_effort=low and analysis is not
// a reasoning task.
func lowEffortReasoning(provider, model string) bool {
	return strings.EqualFold(provider, "zai") && strings.HasPrefix(model, "glm-5.3")
}

// Resources is the resource context injected into the prompt's slots:
// rendered lists from the resource index and the ward vault.
type Resources struct {
	Skills []string
	Agents []string
	MCPs   []string
	Wards  []string
}

// Analyzer runs the one intent call: render the prompt with the
// resource slots, complete, strictly parse. One retry on a parse
// failure, then a typed degradation — analysis never blocks a run.
type Analyzer struct {
	Completer Completer
	// Snapshot supplies the user-owned intent-analysis prompt template.
	Snapshot func() registry.Snapshot
	// Provider and Model select the analysis model; empty provider
	// means "the completer's default".
	Provider string
	Model    string
}

// AnalysisUnavailable is the typed non-fatal degradation: the run
// proceeds without analysis and the reason rides the event.
type AnalysisUnavailable struct {
	Reason string
}

func (e *AnalysisUnavailable) Error() string {
	return "intent: analysis unavailable: " + e.Reason
}

// Analyze runs the packet-driven intent decision for a goal: the
// user-owned prompt (slots filled from the RANKED, CAPPED packet) is
// the system rubric; the user message embeds the request as escaped
// advisory data. Failures re-ask with CORRECTION FEEDBACK (the code +
// the fix — never a blind retry), at most three attempts inside the
// 45s deadline. Validation runs against the packet's ID sets; the
// wrapper's Sanitize remains the belt-and-braces guard.
func (a *Analyzer) Analyze(ctx context.Context, goal string, pkt Packet, provider, model string) (domain.IntentAnalysis, error) {
	prompt, err := a.renderPacketPrompt(pkt)
	if err != nil {
		return domain.IntentAnalysis{}, err // prompt unusable: degrade (embedded fallback already tried)
	}
	ctx, cancel := context.WithTimeout(ctx, analysisDeadline)
	defer cancel()
	complete := func(ctx context.Context, provider, model, system, user string, extra map[string]any) (string, error) {
		if pc, ok := a.Completer.(ParamCompleter); ok && len(extra) > 0 {
			return pc.CompleteWith(ctx, provider, model, system, user, extra)
		}
		return a.Completer.Complete(ctx, provider, model, system, user)
	}
	var extraBody map[string]any
	if _, ok := a.Completer.(ParamCompleter); ok && lowEffortReasoning(provider, model) {
		extraBody = map[string]any{"reasoning_effort": "low"}
	}
	var lastCodes []string
	var lastErr error
	var lastParsed domain.IntentAnalysis
	parsedOnce := false
	for attempt := 0; attempt < 3; attempt++ {
		user := RequestBlock(goal)
		if attempt > 0 {
			user = CorrectionBlock(lastCodes) + "\n\n" + RequestBlock(goal)
		}
		raw, err := complete(ctx, provider, model, prompt, user, extraBody)
		if err != nil {
			if ctx.Err() != nil {
				return domain.IntentAnalysis{}, &AnalysisUnavailable{Reason: "deadline exceeded (45s)"}
			}
			return domain.IntentAnalysis{}, &AnalysisUnavailable{Reason: "model call failed: " + err.Error()}
		}
		analysis, perr := domain.ParseIntentAnalysis(extractJSON(raw))
		if perr != nil {
			lastCodes = []string{"invalid_json"}
			lastErr = perr
			continue
		}
		if codes := pkt.Validate(analysis); len(codes) > 0 {
			// Correction retry; remember the parse — if every retry
			// still names unknowns, the LAST PARSED decision returns
			// (the wrapper's Sanitize drops the residue — a good
			// analysis is not wasted on one phantom ID).
			lastParsed, parsedOnce = analysis, true
			lastCodes = codes
			lastErr = fmt.Errorf("unknown recommendations: %s", strings.Join(codes, ", "))
			continue
		}
		return analysis, nil
	}
	if parsedOnce {
		return lastParsed, nil
	}
	return domain.IntentAnalysis{}, &AnalysisUnavailable{Reason: fmt.Sprintf("decision failed validation 3x: %v", lastErr)}
}

// requiredSlots are the resource-slot markers a user-owned intent
// prompt must carry; a prompt without them cannot receive the runtime
// resource context and is not usable.
var requiredSlots = []string{"{{.Skills}}", "{{.Agents}}", "{{.MCPs}}", "{{.Wards}}"}

// promptData is the template data for the intent prompt.
type promptData struct{ Skills, Agents, MCPs, Wards string }

// renderPacketPrompt renders the prompt with the packet's slot bodies
// (ranked, capped, description-carrying — the host-authoritative
// catalog agentzero's catalog.rs builds).
func (a *Analyzer) renderPacketPrompt(pkt Packet) (string, error) {
	sk, ag, mc, wd := pkt.Render()
	return a.render(promptData{Skills: sk, Agents: ag, MCPs: mc, Wards: wd})
}

// renderPrompt renders the user-owned template with the resource
// slots. A candidate is selected only when it parses, carries every
// required slot, and renders successfully; ANY failure falls back to
// the embedded default (a broken user file degrades analysis to the
// shipped behavior — it never blocks the run).
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
	if a.Snapshot != nil {
		if p, ok := a.Snapshot().Prompts["intent-analysis"]; ok {
			if out, err := render(p.Raw); err == nil && hasAllSlots(p.Raw) && strings.TrimSpace(out) != "" {
				return out, nil
			}
			// broken/shape-invalid/empty user prompt → default below
		}
	}
	out, err := render(defaultPrompt)
	if err != nil {
		return "", &AnalysisUnavailable{Reason: "embedded prompt broken (build bug)"}
	}
	return out, nil
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
