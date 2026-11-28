package config

// The in-run context budget knobs (engine-adk-spine slice 1): how skills
// reach a model, which model writes in-run summaries, and how much one
// tool result may contribute to the context. Each default is an operator
// decision (D3 skills on demand, D4 a cheap summarizer, Q2b a byte cap).

import (
	"fmt"
	"strings"
)

// SkillsConfig selects how provisioned skills reach a model (decision D3):
// progressive = names + descriptions in a `skill` tool, bodies loaded on
// demand (ADK spine only); bodies = full SKILL.md bodies in the prompt (the
// pre-migration behavior, and always on the legacy loop).
type SkillsConfig struct {
	Injection string `yaml:"injection"`
}

// Skill injection modes.
const (
	SkillsProgressive = "progressive"
	SkillsBodies      = "bodies"
)

// DefaultToolResultCap is the in-run cap on one tool result (bytes). The
// solar run's five shell dumps were 25–38 KB each and rode every later
// step (≈350k tokens); 12 KB keeps a usable head and offloads the rest.
// The cap never applies to the delegate fold-back or loaded skills.
const DefaultToolResultCap = 12 * 1024

// applyContextBudgetDefaults fills absent keys and rejects unknown or
// malformed values (strict decode): skills.injection, compaction.summarizer
// (a qualified provider/model-id), compaction.tool_result_cap. The
// summarizer has no default — empty IS the run's own model.
func applyContextBudgetDefaults(cfg *Config, file string) error {
	switch cfg.Skills.Injection {
	case "":
		cfg.Skills.Injection = SkillsProgressive // absent key: on-demand skills on the spine
	case SkillsProgressive, SkillsBodies:
	default:
		return fmt.Errorf("config: %s: skills.injection must be one of %q, %q (got %q)", file, SkillsProgressive, SkillsBodies, cfg.Skills.Injection)
	}
	if ref := cfg.Compaction.Summarizer; ref != "" {
		if p, m, ok := strings.Cut(ref, "/"); !ok || p == "" || m == "" || strings.Contains(m, "/") {
			return fmt.Errorf("config: %s: compaction.summarizer must be a qualified provider/model-id (got %q)", file, ref)
		}
	}
	switch {
	case cfg.Compaction.ToolResultCap == 0:
		cfg.Compaction.ToolResultCap = DefaultToolResultCap
	case cfg.Compaction.ToolResultCap < 0:
		return fmt.Errorf("config: %s: compaction.tool_result_cap must be a positive number of bytes", file)
	}
	return nil
}
