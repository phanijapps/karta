// Package sysprompt composes the system prompt from static registry
// sections (config.yaml system_prompt: role/soul/os/tools/custom) and
// offers the dynamic-section builders the goal-run wrapper appends
// (## Task Analysis, ## Skills, ## Context). It serves chat (static
// only) and goal runs (full stack). Failures are loud and typed: a
// missing role section is a load error naming the file and section;
// missing optional sections are skipped (spec: system-prompt).
//
// Boundaries: imports config + registry (downward, like chat); never
// engine, server, or client code.
package sysprompt

import (
	"fmt"
	"runtime"
	"strings"

	"zbot/backend/internal/config"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// SectionError is a typed composed-prompt failure: a section that must
// resolve could not be read. It names the section and the file.
type SectionError struct {
	Section string
	Name    string
	File    string
}

func (e *SectionError) Error() string {
	file := e.File
	if file == "" {
		file = "config/prompts/" + e.Name + ".md"
	}
	return fmt.Sprintf("sysprompt: section %q (file %s) is missing or empty", e.Section, file)
}

// section renders one registry prompt's raw body, or ("", nil) when
// the section is not configured or its file is absent/empty (skip) —
// per the spec's optional-missing scenario. A body that is only
// whitespace counts as missing: a heading with no content is not a
// section.
func section(label, name string, snap registry.Snapshot) (string, error) {
	if name == "" {
		return "", nil
	}
	p, ok := snap.Prompts[name]
	if !ok || strings.TrimSpace(p.Raw) == "" {
		return "", nil // absent or empty: skip (Compose enforces role)
	}
	return "## " + label + "\n\n" + strings.TrimSpace(p.Raw) + "\n", nil
}

// osSectionName resolves the os config to a registry name. auto picks
// the pack by the daemon's GOOS; unknown platforms resolve to nothing.
func osSectionName(mode string) string {
	switch mode {
	case "", "none":
		return ""
	case "auto":
		switch runtime.GOOS {
		case "linux":
			return "os-linux"
		case "windows":
			return "os-windows"
		case "darwin":
			return "os-darwin"
		}
		return ""
	default:
		return "os-" + mode
	}
}

// Compose assembles the static sections in spec order (Role, Soul, OS,
// Tools, Custom). The role section is REQUIRED: absent from the
// registry is a typed error; every other section skips when missing.
func Compose(cfg config.SystemPromptConfig, snap registry.Snapshot) (string, error) {
	var b strings.Builder

	role, err := section("Role", cfg.Role, snap)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", &SectionError{Section: "Role", Name: cfg.Role, File: promptPath(snap, cfg.Role)}
	}
	b.WriteString(role)

	for _, s := range []struct{ label, name string }{
		{"Soul", cfg.Soul},
		{"OS", osSectionName(cfg.OS)},
		{"Tools", cfg.Tools},
	} {
		out, err := section(s.label, s.name, snap)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
	for _, name := range cfg.Custom {
		out, err := section(name, name, snap)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// TaskAnalysis renders the ## Task Analysis dynamic section from a
// sanitized contract. Fast-path (approach=simple) instructions ride
// along, matching the operator's first-turn protocol idea.
func TaskAnalysis(a domain.IntentAnalysis) string {
	return taskAnalysis(a, false)
}

// TaskAnalysisProgressive is TaskAnalysis for a run whose skills arrive
// through the `skill` tool (engine-adk-spine D3): the recommended skills
// are named as one call away, not as provided.
func TaskAnalysisProgressive(a domain.IntentAnalysis) string {
	return taskAnalysis(a, true)
}

func taskAnalysis(a domain.IntentAnalysis, progressive bool) string {
	var b strings.Builder
	b.WriteString("## Task Analysis\n\n")
	fmt.Fprintf(&b, "- Primary intent: %s\n", a.PrimaryIntent)
	if len(a.HiddenIntents) > 0 {
		fmt.Fprintf(&b, "- Hidden intents: %s\n", strings.Join(a.HiddenIntents, "; "))
	}
	if a.SolutionPath != "" {
		fmt.Fprintf(&b, "- Solution path: %s\n", a.SolutionPath)
	}
	fmt.Fprintf(&b, "- Complexity: %s (step budget honored by the runtime)\n", string(a.Complexity))
	if len(a.RecommendedSkills) > 0 && progressive {
		fmt.Fprintf(&b, "- Skills one call away (load with the karta_skill tool before relying on one): %s\n", strings.Join(a.RecommendedSkills, ", "))
	} else if len(a.RecommendedSkills) > 0 {
		fmt.Fprintf(&b, "- Skills provided in context: %s\n", strings.Join(a.RecommendedSkills, ", "))
	}
	if len(a.RecommendedAgents) > 0 {
		fmt.Fprintf(&b, "- Agents available for delegation: %s\n", strings.Join(a.RecommendedAgents, ", "))
	}
	if len(a.RecommendedMCPs) > 0 {
		fmt.Fprintf(&b, "- MCP tools noted: %s\n", strings.Join(a.RecommendedMCPs, ", "))
	}
	if a.WardRecommendation.WardName != "" {
		fmt.Fprintf(&b, "- Ward: %s (%s) — %s\n", a.WardRecommendation.WardName, string(a.WardRecommendation.Action), a.WardRecommendation.Reason)
	}
	fmt.Fprintf(&b, "- Approach: %s — %s\n", string(a.ExecutionStrategy.Approach), a.ExecutionStrategy.Explanation)
	if a.ExecutionStrategy.Approach == "simple" {
		if progressive {
			b.WriteString("\nFast path: answer directly. Load a recommended skill with the karta_skill tool only if the task needs it; do not plan aloud, do not delegate unless the answer truly needs it.\n")
		} else {
			b.WriteString("\nFast path: answer directly. The runtime already provided the skills above; do not plan aloud, do not delegate unless the answer truly needs it.\n")
		}
	} else {
		b.WriteString("\nGraph path: plan the steps, delegate self-contained tasks to the recommended agents, and verify each result before the next step.\n")
	}
	return b.String()
}

// SkillsProgressive renders the ## Skills section for progressive
// injection (engine-adk-spine D3): no bodies — the names, and the
// instruction to load one with the skill tool before relying on it. Empty
// when nothing is recommended.
func SkillsProgressive(names []string) string {
	if len(names) == 0 {
		return ""
	}
	sorted := append([]string(nil), names...)
	sortStrings(sorted)
	return "## Skills\n\nLoaded on demand through the karta_skill tool (call karta_skill(name) before relying on one): " + strings.Join(sorted, ", ") + "\n"
}

// Skills renders the ## Skills dynamic section: the full bodies of the
// provisioned skills. On the legacy loop (and skills.injection: bodies)
// this is the ONLY way a skill enters a run — the runtime provides, the
// agent does not fetch (spec: system-prompt); the ADK spine offers them
// through the skill tool instead.
func Skills(bodies map[string]string) string {
	if len(bodies) == 0 {
		return ""
	}
	names := make([]string, 0, len(bodies))
	for n := range bodies {
		names = append(names, n)
	}
	sortStrings(names)
	var b strings.Builder
	b.WriteString("## Skills\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\n### Skill: %s\n\n%s\n", n, strings.TrimSpace(bodies[n]))
	}
	return b.String()
}

// Context renders the ## Context dynamic section from recalled memory
// and knowledge. Empty recall renders no section.
func Context(memory, knowledge []string) string {
	if len(memory) == 0 && len(knowledge) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Context\n")
	if len(memory) > 0 {
		b.WriteString("\nRecalled memory:\n")
		for _, m := range memory {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}
	if len(knowledge) > 0 {
		b.WriteString("\nRelated knowledge:\n")
		for _, k := range knowledge {
			fmt.Fprintf(&b, "- %s\n", k)
		}
	}
	return b.String()
}

// promptPath reports the on-disk path of a registry prompt when known
// (for typed errors).
func promptPath(snap registry.Snapshot, name string) string {
	if p, ok := snap.Prompts[name]; ok {
		return p.Path
	}
	return ""
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
