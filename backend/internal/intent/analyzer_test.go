package intent

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
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
	mu          sync.Mutex
	responses   []string // consumed per call
	errs        []error
	usage       []Usage // per call (zero when unlisted)
	calls       int
	lastSystem  string
	lastUser    string
	extraBodies []map[string]any
}

func (f *fakeCompleter) record(system, user string, extra map[string]any) {
	f.lastSystem, f.lastUser = system, user
	f.extraBodies = append(f.extraBodies, extra)
}

func (f *fakeCompleter) Complete(_ context.Context, _, _, system, user string) (string, error) {
	s, _, err := f.complete(system, user, nil)
	return s, err
}

// CompleteWith satisfies ParamCompleter (provider-param coverage).
func (f *fakeCompleter) CompleteWith(_ context.Context, _, _, system, user string, extra map[string]any) (string, error) {
	s, _, err := f.complete(system, user, extra)
	return s, err
}

// CompleteUsage satisfies UsageCompleter (the daemon's accounting seam).
func (f *fakeCompleter) CompleteUsage(_ context.Context, _, _, system, user string, extra map[string]any) (string, Usage, error) {
	return f.complete(system, user, extra)
}

func (f *fakeCompleter) complete(system, user string, extra map[string]any) (string, Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(system, user, extra)
	i := f.calls
	f.calls++
	var u Usage
	if i < len(f.usage) {
		u = f.usage[i]
	}
	if i < len(f.errs) && f.errs[i] != nil {
		return "", u, f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], u, nil
	}
	return "", u, nil
}

const validContract = `{"primary_intent":"x","hidden_intents":[],"solution_path":"","complexity":"S",
"recommended_skills":["clean-code"],"recommended_agents":["root"],"recommended_mcp_tools":[],
"ward_recommendation":{"action":"create_new","ward_name":"test-ward","reason":"r"},
"execution_strategy":{"approach":"simple","explanation":"e"}}`

func TestAnalyzer(t *testing.T) {
	noopSnap := func() registry.Snapshot { return registry.Snapshot{} }
	// The packet mirrors the old Resources content: entries with
	// descriptions, host-authoritative.
	res := Packet{Skills: []CatalogEntry{{ID: "clean-code", Description: "coding standards"}}, Wards: []CatalogEntry{{ID: "stock-analysis", Description: "equity research"}}}

	t.Run("valid contract parses", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{validContract}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		got, err := a.Analyze(context.Background(), "do the thing", res, "p", "m", Limits{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if got.Analysis.Complexity != domain.ComplexityS || got.Analysis.WardRecommendation.WardName != "test-ward" {
			t.Fatalf("contract: %+v", got)
		}
		if !strings.Contains(fc.lastSystem, "- clean-code — coding standards") || !strings.Contains(fc.lastSystem, "- stock-analysis — equity research") {
			t.Fatalf("packet slots not injected:\n%s", fc.lastSystem)
		}
		if !strings.Contains(fc.lastUser, "do the thing") {
			t.Fatal("goal not embedded in the user request block")
		}
	})

	t.Run("parse failure retries once then degrades typed", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{"not json", "still not json", "nope"}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		_, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{})
		var du *AnalysisUnavailable
		if !errors.As(err, &du) {
			t.Fatalf("want typed degradation, got %v", err)
		}
		if fc.calls != 3 {
			t.Fatalf("want correction feedback up to 3 attempts, got %d calls", fc.calls)
		}
	})

	t.Run("retry succeeds on second attempt", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{"garbage", validContract}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		if _, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{}); err != nil {
			t.Fatalf("retry must succeed: %v", err)
		}
	})

	t.Run("model failure degrades typed without retry", func(t *testing.T) {
		fc := &fakeCompleter{errs: []error{errors.New("provider down")}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		_, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{})
		var du *AnalysisUnavailable
		if !errors.As(err, &du) || !strings.Contains(du.Reason, "provider down") {
			t.Fatalf("want model-failure degradation, got %v", err)
		}
	})

	t.Run("code-fenced JSON is extracted", func(t *testing.T) {
		fenced := "```json\n" + validContract + "\n```"
		fc := &fakeCompleter{responses: []string{fenced}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		if _, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{}); err != nil {
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
		if _, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{}); err != nil {
			t.Fatalf("broken user prompt must fall back: %v", err)
		}
	})

	t.Run("user-owned prompt with all slots is used", func(t *testing.T) {
		snap := registry.Snapshot{}
		snap.Prompts = map[string]registry.Prompt{
			"intent-analysis": {Name: "intent-analysis", Raw: "CUSTOM MARKER s={{.Skills}} a={{.Agents}} m={{.MCPs}} w={{.Wards}}"},
		}
		fc := &fakeCompleter{responses: []string{validContract}}
		a := Analyzer{Completer: fc, Snapshot: func() registry.Snapshot { return snap }}
		if _, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{}); err != nil {
			t.Fatalf("custom prompt must render: %v", err)
		}
		if !strings.Contains(fc.lastSystem, "CUSTOM MARKER") || !strings.Contains(fc.lastSystem, "clean-code") {
			t.Fatalf("custom prompt not used:\n%s", fc.lastSystem)
		}
	})

	t.Run("unusable user prompts all fall back to the embedded default", func(t *testing.T) {
		cases := map[string]string{
			"empty":             "",
			"contract-free":     "just prose, no slots at all",
			"missing slot":      "s={{.Skills}} a={{.Agents}} m={{.MCPs}}",
			"syntax invalid":    "broken {{.Skills",
			"execution invalid": "{{.Unknown}}",
		}
		for name, raw := range cases {
			t.Run(name, func(t *testing.T) {
				snap := registry.Snapshot{}
				snap.Prompts = map[string]registry.Prompt{
					"intent-analysis": {Name: "intent-analysis", Raw: raw},
				}
				fc := &fakeCompleter{responses: []string{validContract}}
				a := Analyzer{Completer: fc, Snapshot: func() registry.Snapshot { return snap }}
				if _, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{}); err != nil {
					t.Fatalf("unusable prompt must fall back, not fail: %v", err)
				}
				if !strings.Contains(fc.lastSystem, "You are an intent analyzer") {
					t.Fatalf("embedded default not used for %s:\n%s", name, fc.lastSystem)
				}
			})
		}
	})

	t.Run("empty model output degrades typed", func(t *testing.T) {
		fc := &fakeCompleter{responses: []string{""}}
		a := Analyzer{Completer: fc, Snapshot: noopSnap}
		_, err := a.Analyze(context.Background(), "goal", res, "p", "m", Limits{})
		var du *AnalysisUnavailable
		if !errors.As(err, &du) {
			t.Fatalf("empty model output must degrade typed, got %v", err)
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
