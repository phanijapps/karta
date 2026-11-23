package domain

import (
	"strings"
	"testing"
)

func TestParseIntentAnalysis(t *testing.T) {
	t.Run("full contract decodes", func(t *testing.T) {
		raw := `{
  "primary_intent": "compare-cloud-providers",
  "hidden_intents": ["include cost table"],
  "solution_path": "gather data, build table, compare",
  "complexity": "L",
  "recommended_skills": ["clean-code"],
  "recommended_agents": ["research-agent"],
  "recommended_mcp_tools": [],
  "ward_recommendation": {"action": "use_existing", "ward_name": "cloud-research", "reason": "domain match"},
  "execution_strategy": {"approach": "graph", "explanation": "multi-step"}
}`
		a, err := ParseIntentAnalysis(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if a.Complexity != ComplexityL || a.WardRecommendation.Action != WardUseExisting || a.ExecutionStrategy.Approach != ApproachGraph {
			t.Fatalf("contract: %+v", a)
		}
		if a.WardRecommendation.WardName != "cloud-research" || a.SolutionPath == "" {
			t.Fatalf("fields: %+v", a)
		}
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		if _, err := ParseIntentAnalysis(`{"primary_intent": "x", "surprise": 1}`); err == nil {
			t.Fatal("unknown field must fail strict decode")
		}
	})

	t.Run("envelope rejected", func(t *testing.T) {
		if _, err := ParseIntentAnalysis(`{"intent": {"primary_intent": "x"}}`); err == nil {
			t.Fatal("an envelope must not decode as the contract")
		}
	})

	t.Run("empty partial null trailing all rejected", func(t *testing.T) {
		valid := `{"primary_intent":"x","complexity":"S","recommended_skills":[],"recommended_agents":[],
"recommended_mcp_tools":[],"ward_recommendation":{"action":"create_new","ward_name":"ok-ward"},
"execution_strategy":{"approach":"simple"}}`
		for name, raw := range map[string]string{
			"empty":            "",
			"null":             "null",
			"empty object":     "{}",
			"partial":          `{"primary_intent": "x"}`,
			"missing ward":     `{"primary_intent":"x","complexity":"S","execution_strategy":{"approach":"simple"}}`,
			"trailing value":   valid + ` {"another": 1}`,
			"trailing garbage": valid + " !!!",
		} {
			if _, err := ParseIntentAnalysis(raw); err == nil {
				t.Fatalf("%s must be rejected", name)
			}
		}
	})
}

func TestComplexityBudget(t *testing.T) {
	for tc, want := range map[IntentComplexity]int{ComplexityS: 4, ComplexityM: 8, ComplexityL: 16, ComplexityXL: 24} {
		if got, err := tc.StepBudget(); err != nil || got != want {
			t.Fatalf("%s: got %d %v want %d", tc, got, err, want)
		}
	}
	if _, err := IntentComplexity("H").StepBudget(); err == nil {
		t.Fatal("unknown complexity must error")
	}
}

func TestSanitize(t *testing.T) {
	known := func(want ...string) func(string) bool {
		set := map[string]bool{}
		for _, w := range want {
			set[w] = true
		}
		return func(n string) bool { return set[n] }
	}

	t.Run("hallucinated names dropped and reported", func(t *testing.T) {
		a := IntentAnalysis{
			RecommendedSkills:  []string{"clean-code", "ghost-skill"},
			RecommendedAgents:  []string{"research-agent", "phantom-agent"},
			RecommendedMCPs:    []string{"ghost-mcp"},
			Complexity:         ComplexityS,
			WardRecommendation: WardRecommendation{Action: WardCreateNew, WardName: "market-research"},
		}
		got, dropped := a.Sanitize(known("clean-code"), known("research-agent"), known(), known())
		if len(got.RecommendedSkills) != 1 || got.RecommendedSkills[0] != "clean-code" {
			t.Fatalf("skills: %+v", got.RecommendedSkills)
		}
		if len(got.RecommendedAgents) != 1 {
			t.Fatalf("agents: %+v", got.RecommendedAgents)
		}
		if len(got.RecommendedMCPs) != 0 {
			t.Fatalf("mcps: %+v", got.RecommendedMCPs)
		}
		joined := strings.Join(dropped, ",")
		for _, want := range []string{"ghost-skill", "phantom-agent", "ghost-mcp"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("drop %s not reported: %v", want, dropped)
			}
		}
	})

	t.Run("use_existing with unknown ward degrades to create_new", func(t *testing.T) {
		a := IntentAnalysis{WardRecommendation: WardRecommendation{Action: WardUseExisting, WardName: "not-listed"}}
		got, _ := a.Sanitize(nil, nil, nil, known("other-ward"))
		if got.WardRecommendation.Action != WardCreateNew {
			t.Fatalf("action: %+v", got.WardRecommendation)
		}
	})

	t.Run("use_existing with exact match kept", func(t *testing.T) {
		a := IntentAnalysis{WardRecommendation: WardRecommendation{Action: WardUseExisting, WardName: "stock-analysis"}}
		got, _ := a.Sanitize(nil, nil, nil, known("stock-analysis"))
		if got.WardRecommendation.Action != WardUseExisting {
			t.Fatalf("action: %+v", got.WardRecommendation)
		}
	})

	t.Run("task-specific ward name rejected", func(t *testing.T) {
		for _, bad := range []string{"AMD-Stock", "spy_options", "x", strings.Repeat("a", 65)} {
			if ValidWardName(bad) {
				t.Fatalf("%q must be invalid", bad)
			}
		}
		for _, good := range []string{"stock-analysis", "india-analysis", "personal-life", "a1-research"} {
			if !ValidWardName(good) {
				t.Fatalf("%q must be valid", good)
			}
		}
		a := IntentAnalysis{WardRecommendation: WardRecommendation{Action: WardCreateNew, WardName: "AMD-Stock"}}
		got, dropped := a.Sanitize(nil, nil, nil, nil)
		if got.WardRecommendation.WardName != "" || got.WardRecommendation.Action != WardCreateNew {
			t.Fatalf("ward: %+v", got.WardRecommendation)
		}
		if len(dropped) != 1 {
			t.Fatalf("drops: %v", dropped)
		}
	})

	t.Run("invalid approach and complexity fall back", func(t *testing.T) {
		a := IntentAnalysis{
			Complexity:         "H",
			ExecutionStrategy:  IntentExecutionPlan{Approach: "agentic"},
			WardRecommendation: WardRecommendation{Action: WardCreateNew, WardName: "ok-ward"},
		}
		got, _ := a.Sanitize(nil, nil, nil, nil)
		if got.Complexity != ComplexityM || got.ExecutionStrategy.Approach != ApproachSimple {
			t.Fatalf("fallbacks: %+v", got)
		}
	})
}
