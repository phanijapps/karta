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
func (a *Analyzer) Analyze(ctx context.Context, goal string, res Resources) (domain.IntentAnalysis, error) {
	prompt, err := a.renderPrompt(res)
	if err != nil {
		return domain.IntentAnalysis{}, err // prompt unusable: degrade (embedded fallback already tried)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ { // one retry on parse failure
		raw, err := a.Completer.Complete(ctx, a.Provider, a.Model, "", prompt+"\n\n## User Request\n"+goal)
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

// renderPrompt renders the user-owned template with the resource
// slots; the embedded default is the fallback for an absent or
// broken template (a broken user file never blocks analysis).
func (a *Analyzer) renderPrompt(res Resources) (string, error) {
	tmplText := defaultPrompt
	if a.Snapshot != nil {
		snap := a.Snapshot()
		if p, ok := snap.Prompts["intent-analysis"]; ok {
			if _, err := template.New("intent-analysis").Parse(p.Raw); err == nil {
				tmplText = p.Raw // user-owned prompt, validated parseable
			}
			// A broken user file falls back to the embedded default —
			// analysis degrades to the shipped behavior, never blocks.
		}
	}
	tmpl, err := template.New("intent").Parse(tmplText)
	if err != nil {
		return "", &AnalysisUnavailable{Reason: "prompt template broken"}
	}
	var b strings.Builder
	data := struct{ Skills, Agents, MCPs, Wards string }{
		Skills: renderListBody(res.Skills),
		Agents: renderListBody(res.Agents),
		MCPs:   renderListBody(res.MCPs),
		Wards:  renderListBody(res.Wards),
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return "", &AnalysisUnavailable{Reason: "prompt render failed: " + err.Error()}
	}
	return b.String(), nil
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

// extractJSON pulls the first top-level JSON object out of a model
// response (models sometimes fence code blocks despite instructions).
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = strings.TrimPrefix(s[i:], "```")
		if j := strings.Index(s, "\n"); j >= 0 {
			s = s[j+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}
