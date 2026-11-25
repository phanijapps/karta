package eino

import (
	"zbot/backend/internal/domain"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/wardtools"
)

// runInput is one run's wiring: session identity, resolved model +
// credential, delegation posture, and ward hosting. Declarative state
// — extracted from adapter.go (LOC budget).

type runInput struct {
	sessionID, execID, goal      string
	providerName, modelID, token string
	prov                         providers.Provider
	maxSteps                     int // per-run bound (clamped to MaxSteps; 0 = default)

	// Delegation wiring: agent-shaped runs and children.
	systemPrompt string   // resolved system prompt (agent body or default)
	toolAllow    []string // agent allowlist (nil = all tools)
	agentName    string   // "" for root default wiring
	depth        int      // 0 = root
	grant        int      // may delegate while depth < grant (agent max_depth; root = cap)
	isChild      bool     // children surface steps as delegation events

	// Ward hosting (slice 3): the session's sticky host; nil = wards
	// off. Root runs get the ward tools; children get the file tools +
	// the Active Ward packet on their prompt (read + declared roles).
	wardHost *wardtools.Host
}

// agent names the acting agent on rows and events: "root" for the root
// run (a named root run uses its agent's name), the sub-agent's name
// for children. Provider/model is provenance, not an agent.
func (in runInput) agent() string {
	if in.agentName != "" {
		return in.agentName
	}
	return domain.AgentRoot
}
