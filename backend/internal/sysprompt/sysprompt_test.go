package sysprompt

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"zbot/backend/internal/config"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

func snapWith(prompts map[string]string) registry.Snapshot {
	snap := registry.Snapshot{}
	snap.Prompts = map[string]registry.Prompt{}
	for name, raw := range prompts {
		snap.Prompts[name] = registry.Prompt{Name: name, Raw: raw}
	}
	return snap
}

func TestCompose(t *testing.T) {
	snap := snapWith(map[string]string{
		"role":       "You are Karta.",
		"soul":       "Direct and precise.",
		"os-linux":   "Platform: Linux.",
		"tools":      "Use the tools you have.",
		"house":      "House rules body.",
		"os-windows": "Platform: Windows.",
	})

	t.Run("full assembly in order with auto-OS", func(t *testing.T) {
		cfg := config.SystemPromptConfig{Role: "role", Soul: "soul", OS: "auto", Tools: "tools", Custom: []string{"house"}}
		if goos != "linux" {
			t.Skipf("auto-OS pick is GOOS-sensitive (running %s)", goos)
		}
		got, err := Compose(cfg, snap)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		for _, want := range []string{"## Role", "## Soul", "## OS", "## Tools", "## house", "You are Karta.", "Direct and precise."} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "Windows") {
			t.Fatal("auto must pick the daemon's platform pack")
		}
		if strings.Index(got, "## Role") > strings.Index(got, "## Soul") ||
			strings.Index(got, "## Soul") > strings.Index(got, "## OS") ||
			strings.Index(got, "## OS") > strings.Index(got, "## Tools") {
			t.Fatal("section order wrong")
		}
	})

	t.Run("missing role is a typed error", func(t *testing.T) {
		cfg := config.SystemPromptConfig{Role: "absent-role"}
		_, err := Compose(cfg, snap)
		var se *SectionError
		if !errors.As(err, &se) || se.Name != "absent-role" || se.Section != "Role" {
			t.Fatalf("want typed SectionError, got %v", err)
		}
	})

	t.Run("missing optional sections skip", func(t *testing.T) {
		cfg := config.SystemPromptConfig{Role: "role", Soul: "absent-soul", Tools: "absent-tools"}
		got, err := Compose(cfg, snap)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		if !strings.Contains(got, "## Role") || strings.Contains(got, "Soul") {
			t.Fatalf("optional skips: %q", got)
		}
	})

	t.Run("os none and unknown explicit platform skip", func(t *testing.T) {
		for _, mode := range []string{"none", "plan9"} {
			cfg := config.SystemPromptConfig{Role: "role", OS: mode}
			got, err := Compose(cfg, snap)
			if err != nil || strings.Contains(got, "## OS") {
				t.Fatalf("os %q: %q %v", mode, got, err)
			}
		}
	})
}

var goos = runtime.GOOS

func TestTaskAnalysisSection(t *testing.T) {
	a := domain.IntentAnalysis{
		PrimaryIntent:     "compare-providers",
		Complexity:        domain.ComplexityL,
		RecommendedSkills: []string{"clean-code"},
		WardRecommendation: domain.WardRecommendation{
			Action: domain.WardUseExisting, WardName: "cloud-research", Reason: "domain match",
		},
		ExecutionStrategy: domain.IntentExecutionPlan{Approach: domain.ApproachGraph, Explanation: "multi-agent"},
	}
	got := TaskAnalysis(a)
	for _, want := range []string{"## Task Analysis", "compare-providers", "L", "clean-code", "cloud-research", "use_existing", "graph", "Graph path"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	fast := TaskAnalysis(domain.IntentAnalysis{ExecutionStrategy: domain.IntentExecutionPlan{Approach: domain.ApproachSimple}})
	if !strings.Contains(fast, "Fast path") {
		t.Fatalf("fast-path instructions missing:\n%s", fast)
	}
}

func TestSkillsAndContextSections(t *testing.T) {
	got := Skills(map[string]string{"zeta": "Zeta body.", "alpha": "Alpha body."})
	if !strings.HasPrefix(got, "## Skills") || strings.Index(got, "Alpha body.") > strings.Index(got, "Zeta body.") {
		t.Fatalf("skills section: %q", got)
	}
	if Skills(nil) != "" {
		t.Fatal("no skills must render no section")
	}
	ctx := Context([]string{"memory one"}, []string{"entity: ACME"})
	if !strings.Contains(ctx, "Recalled memory") || !strings.Contains(ctx, "Related knowledge") {
		t.Fatalf("context: %q", ctx)
	}
	if Context(nil, nil) != "" {
		t.Fatal("no recall must render no section")
	}
}
