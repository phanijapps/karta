package intent

import (
	"context"
	_ "embed"
	"strings"
	"text/template"

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

// Analyze runs the intent call for a goal. It returns the parsed
// contract (unsanitized — the wrapper validates against the live
// index). The system prompt is empty: the analysis prompt IS the
// instruction.
func (a *Analyzer) Analyze(ctx context.Context, goal string, res Resources, provider, model string) (domain.IntentAnalysis, error) {
	prompt, err := a.renderPrompt(res)
	if err != nil {
		return domain.IntentAnalysis{}, err // prompt unusable: degrade (embedded fallback already tried)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ { // one retry on parse failure
		raw, err := a.Completer.Complete(ctx, provider, model, "", prompt+"\n\n## User Request\n"+goal)
		if err != nil {
			return domain.IntentAnalysis{}, &AnalysisUnavailable{Reason: "model call failed: " + err.Error()}
		}
		analysis, perr := domain.ParseIntentAnalysis(extractJSON(raw))
		if perr == nil {
			return analysis, nil
		}
		lastErr = perr
	}
	return domain.IntentAnalysis{}, &AnalysisUnavailable{Reason: "contract parse failed twice: " + lastErr.Error()}
}

// requiredSlots are the resource-slot markers a user-owned intent
// prompt must carry; a prompt without them cannot receive the runtime
// resource context and is not usable.
var requiredSlots = []string{"{{.Skills}}", "{{.Agents}}", "{{.MCPs}}", "{{.Wards}}"}

// promptData is the template data for the intent prompt.
type promptData struct{ Skills, Agents, MCPs, Wards string }

// renderPrompt renders the user-owned template with the resource
// slots. A candidate is selected only when it parses, carries every
// required slot, and renders successfully; ANY failure falls back to
// the embedded default (a broken user file degrades analysis to the
// shipped behavior — it never blocks the run).
func (a *Analyzer) renderPrompt(res Resources) (string, error) {
	data := promptData{
		Skills: renderListBody(res.Skills),
		Agents: renderListBody(res.Agents),
		MCPs:   renderListBody(res.MCPs),
		Wards:  renderListBody(res.Wards),
	}
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

// renderListBody renders one slot's list body (without the section
// header, which lives in the template).
func renderListBody(items []string) string {
	if len(items) == 0 {
		return "(none listed)"
	}
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = "- " + it
	}
	return strings.Join(lines, "\n")
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
