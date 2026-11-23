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
}

func (e *SectionError) Error() string {
	return fmt.Sprintf("sysprompt: section %q (prompt %q) is missing from the registry", e.Section, e.Name)
}

// section renders one registry prompt's raw body, or ("", nil) when
// the section is not configured (skip) — a configured optional name
// whose file is absent also skips, per the spec's optional-missing
// scenario.
func section(label, name string, snap registry.Snapshot) (string, error) {
	if name == "" {
		return "", nil
	}
	p, ok := snap.Prompts[name]
	if !ok {
		return "", nil // optional section absent: skip
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
		return "", &SectionError{Section: "Role", Name: cfg.Role}
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
	if len(a.RecommendedSkills) > 0 {
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
		b.WriteString("\nFast path: answer directly. The runtime already provided the skills above; do not plan aloud, do not delegate unless the answer truly needs it.\n")
	} else {
		b.WriteString("\nGraph path: plan the steps, delegate self-contained tasks to the recommended agents, and verify each result before the next step.\n")
	}
	return b.String()
}

// Skills renders the ## Skills dynamic section: the full bodies of the
// provisioned skills. This is the ONLY way a skill enters a run —
// there is no load_skill tool; the runtime provides, the agent does
// not fetch (spec: system-prompt).
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

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
