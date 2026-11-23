package intent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

func TestRouterBypass(t *testing.T) {
	r := Router{}
	cases := []struct {
		goal string
		want bool
	}{
		{"hi", true},
		{"Hello!", true},
		{"thanks, that worked", false}, // clause attached → analyze (conservative)
		{"ok", true},
		{"how are you?", true},
		{"", true},
		{"what's the latest on AMD earnings", false},
		{"summarize this", false},
		{"hello, please analyze my portfolio", false}, // greeting + real ask → analyze
		{"hi there, can you help me research cloud costs", false},
	}
	for _, tc := range cases {
		if got := r.Bypass(tc.goal); got != tc.want {
			t.Errorf("Bypass(%q) = %v, want %v", tc.goal, got, tc.want)
		}
	}
}

// fakeCompleter scripts the model call.
type fakeCompleter struct {
	responses []string // consumed per call
	errs      []error
	calls     int
	lastInput string
}

func (f *fakeCompleter) Complete(_ context.Context, _, _, _, user string) (string, error) {
	f.lastInput = user
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return "", nil
}

const validContract = `{"primary_intent":"x","hidden_intents":[],"solution_path":"","complexity":"S",
"recommended_skills":["clean-code"],"recommended_agents":["root"],"recommended_mcp_tools":[],
"ward_recommendation":{"action":"create_new","ward_name":"test-ward","reason":"r"},
"execution_strategy":{"approach":"simple","explanation":"e"}}`

func TestAnalyzer(t *testing.T) {
	noopSnap := func() registry.Snapshot { return registry.Snapshot{} }
	res := Resources{Skills: []string{"clean-code — coding standards"}, Wards: []string{"stock-analysis — equity research"}}

	t.Run("valid contract parses", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{validContract}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		got, err := a.Analyze(context.Background(), "do the thing", res)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if got.Complexity != domain.ComplexityS || got.WardRecommendation.WardName != "test-ward" {
			t.Fatalf("contract: %+v", got)
		}
		if !strings.Contains(fc.lastInput, "- clean-code — coding standards") || !strings.Contains(fc.lastInput, "- stock-analysis — equity research") {
			t.Fatalf("resource slots not injected:\n%s", fc.lastInput)
		}
		if !strings.Contains(fc.lastInput, "do the thing") {
			t.Fatal("goal not appended")
		}
	})

	t.Run("parse failure retries once then degrades typed", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{"not json", "still not json"}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		_, err := a.Analyze(context.Background(), "goal", res)
		var du *AnalysisUnavailable
		if !errors.As(err, &du) {
			t.Fatalf("want typed degradation, got %v", err)
		}
		if fc.calls != 2 {
			t.Fatalf("want one retry, got %d calls", fc.calls)
		}
	})

	t.Run("retry succeeds on second attempt", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{"garbage", validContract}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		if _, err := a.Analyze(context.Background(), "goal", res); err != nil {
			t.Fatalf("retry must succeed: %v", err)
		}
	})

	t.Run("model failure degrades typed without retry", func(t *testing.T) {
		fc := &fakeCompleter{errs: []error{errors.New("provider down")}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		_, err := a.Analyze(context.Background(), "goal", res)
		var du *AnalysisUnavailable
		if !errors.As(err, &du) || !strings.Contains(du.Reason, "provider down") {
			t.Fatalf("want model-failure degradation, got %v", err)
		}
	})

	t.Run("code-fenced JSON is extracted", func(t *testing.T) {
		fenced := "```json\n" + validContract + "\n```"
		fc := &fakeCompleter{responses: []string{fenced}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		if _, err := a.Analyze(context.Background(), "goal", res); err != nil {
			t.Fatalf("fenced contract must parse: %v", err)
		}
	})

	t.Run("user-owned broken prompt falls back to embedded default", func(t *testing.T) {
		snap := registry.Snapshot{}
		snap.Prompts = map[string]registry.Prompt{
			"intent-analysis": {Name: "intent-analysis", Raw: "broken {{.Skills"},
		}
		fc := &fakeCompleter{responses: []string{validContract}}
		a := Analyzer{Completer: fc, Snapshot: func() registry.Snapshot { return snap }}
		if _, err := a.Analyze(context.Background(), "goal", res); err != nil {
			t.Fatalf("broken user prompt must fall back: %v", err)
		}
	})

	t.Run("user-owned prompt with slots is used", func(t *testing.T) {
		snap := registry.Snapshot{}
		snap.Prompts = map[string]registry.Prompt{
			"intent-analysis": {Name: "intent-analysis", Raw: "CUSTOM MARKER skills={{.Skills}} wards={{.Wards}}"},
		}
		fc := &fakeCompleter{responses: []string{validContract}}
		a := Analyzer{Completer: fc, Snapshot: func() registry.Snapshot { return snap }}
		if _, err := a.Analyze(context.Background(), "goal", res); err != nil {
			t.Fatalf("custom prompt must render: %v", err)
		}
		if !strings.Contains(fc.lastInput, "CUSTOM MARKER") || !strings.Contains(fc.lastInput, "clean-code") {
			t.Fatalf("custom prompt not used:\n%s", fc.lastInput)
		}
	})
}

// The embedded runtime fallback must stay identical to the seeded
// user-owned file — drift here means fallback and seed disagree.
func TestDefaultPromptInSync(t *testing.T) {
	seed, err := os.ReadFile("../config/seedassets/prompts/intent-analysis.md")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if string(seed) != defaultPrompt {
		t.Fatal("internal/intent/default_prompt.md drifted from config/seedassets/prompts/intent-analysis.md — keep them identical")
	}
}
