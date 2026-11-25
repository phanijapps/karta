package registry

// System-role agents (intent-analyst-agent S1): reserved names that are
// real roster entries — listed in Settings → Agents, user-owned files
// with editable prompts and model overrides — but host-run decision
// steps rather than workers. Two rules follow, enforced where each
// applies: never offered for delegation (the engine's routing table and
// its Exec refusal), and never part of the intent analyst's own catalog
// packet (an analyst cannot recommend itself).

// SystemAgentIntentAnalyst is the pre-execution intent analyst.
const SystemAgentIntentAnalyst = "intent-analyst"

// systemAgentNames is the reserved set.
var systemAgentNames = map[string]struct{}{
	SystemAgentIntentAnalyst: {},
}

// IsSystemAgent reports whether name is a reserved system-role agent.
func IsSystemAgent(name string) bool {
	_, ok := systemAgentNames[name]
	return ok
}
