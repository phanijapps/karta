package config_test

// The registry round-trip lives in the EXTERNAL test package on purpose:
// package config's internal tests stay free of sibling-package imports
// (the dependency graph's config-only-domain reading); this integration
// seam uses only the exported surface (NewPaths/Bootstrap/registry).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zbot/backend/internal/config"
	"zbot/backend/internal/registry"
)

var (
	wantSeededSkills = []string{"book-reader", "clean-code", "eagle-eye", "explain-like-socrates", "humanizer"}
	wantSeededAgents = []string{
		"adversarial-reviewer", "builder-agent", "planner-agent",
		"research-agent", "reviewer-agent", "writing-agent",
	}
)

// Every seeded SKILL.md must pass the registry's STRICT loader (the
// same parse it gets at load time — exercised here over what Bootstrap
// actually wrote) and carry a bounded description (the mutation API
// would reject longer ones on any future edit).
func TestSeededSkillsPassStrictLoader(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if err := config.Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	for _, name := range wantSeededSkills {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(paths.SkillsDir, name, "SKILL.md"))
			if err != nil {
				t.Fatalf("read seeded skill: %v", err)
			}
			skillName, desc, _, err := registry.ParseSkillMD(string(raw))
			if err != nil {
				t.Fatalf("strict parse: %v", err)
			}
			if skillName != name {
				t.Fatalf("frontmatter name %q != directory %q", skillName, name)
			}
			if len(desc) > 512 {
				t.Fatalf("description %d chars exceeds the 512 bound the mutation API enforces", len(desc))
			}
		})
	}
}

// Every embedded agent seed must decode via the agent frontmatter
// loader with the intended portable shape: no model pin, no tools
// key (all tools), max_depth 0, a description (delegatability).
func TestSeedAgentsDecode(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if err := config.Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	reg := registry.New()
	snap, errs := reg.LoadAll(registry.Dirs{Agents: paths.AgentsDir, Skills: paths.SkillsDir})
	if len(errs) > 0 {
		t.Fatalf("load errors over a fresh home: %+v", errs)
	}
	// The example roster plus the seeded system agent (the intent analyst).
	if len(snap.Agents) != len(wantSeededAgents)+1 {
		t.Fatalf("agent count: %d want %d (%+v)", len(snap.Agents), len(wantSeededAgents)+1, snap.Agents)
	}
	analyst, ok := snap.Agents[registry.SystemAgentIntentAnalyst]
	if !ok {
		t.Fatalf("the intent analyst must be seeded: %+v", snap.Agents)
	}
	if analyst.Tools == nil || len(analyst.Tools) != 0 {
		t.Fatalf("analyst tools must be an explicit empty list: %#v", analyst.Tools)
	}
	if analyst.MaxDepth != 0 || analyst.Model != "" || analyst.Prompt != "intent-analysis" || analyst.Description == "" || analyst.DisplayName != "Intent Analyst" {
		t.Fatalf("analyst spec: %+v", analyst)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir("prompts"), analyst.Prompt+".md")); err != nil {
		t.Fatalf("the analyst references prompt %q which must be seeded: %v", analyst.Prompt, err)
	}
	if !registry.IsSystemAgent(analyst.Name) {
		t.Fatal("the analyst must be a system agent")
	}
	if len(snap.Skills) != len(wantSeededSkills) {
		t.Fatalf("skill count: %d want %d", len(snap.Skills), len(wantSeededSkills))
	}
	for _, name := range wantSeededAgents {
		spec, ok := snap.Agents[name]
		if !ok {
			t.Fatalf("agent %q missing from snapshot", name)
		}
		if spec.Model != "" {
			t.Fatalf("agent %q pins a model (%q); seeds use the caller's model", name, spec.Model)
		}
		if spec.MaxDepth != 0 {
			t.Fatalf("agent %q grants depth %d; example agents delegate to nobody", name, spec.MaxDepth)
		}
		if spec.Description == "" {
			t.Fatalf("agent %q lacks a description (not delegatable)", name)
		}
		if spec.SystemPrompt == "" {
			t.Fatalf("agent %q lacks a system prompt", name)
		}
		// Seeds omit the tools key entirely (all tools) — assert on the
		// raw file so a future `tools:` line cannot slip in unnoticed.
		raw, err := os.ReadFile(filepath.Join(paths.AgentsDir, name+".example.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "\ntools:") {
			t.Fatalf("agent %q declares a tools key; seeds grant all tools by omission", name)
		}
	}
	for _, name := range wantSeededSkills {
		spec, ok := snap.Skills[name]
		if !ok {
			t.Fatalf("skill %q missing from snapshot", name)
		}
		if spec.Origin != "local" {
			t.Fatalf("skill %q origin %q; seeds are local", name, spec.Origin)
		}
	}
}

// The registry loads the seeded skills from the NEW top-level root
// (relocate-skills-hooks) — external package keeps config sibling-free.
func TestRegistryLoadsTopLevelSkillsRoot(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if err := config.Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	reg := registry.New()
	snap, errs := reg.LoadAll(registry.Dirs{Agents: paths.AgentsDir, Skills: paths.SkillsDir, Hooks: paths.HooksDir})
	if len(errs) > 0 {
		t.Fatalf("load errors: %+v", errs)
	}
	if len(snap.Skills) != 5 {
		t.Fatalf("seeded skills at the new root: %d", len(snap.Skills))
	}
}
