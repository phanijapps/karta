// Package engine defines the seams between the daemon and agent engine
// implementations: tool providers (what the engine may offer and execute)
// and agent resolution (registry-driven definitions). Framework-free —
// only the adapter packages under engine/<name>/ import frameworks.
package engine

import (
	"context"
	"fmt"
	"strings"

	"zbot/backend/internal/registry"
	"zbot/backend/internal/tools"
)

// AgentDef is a resolved agent definition for one run.
type AgentDef struct {
	Name         string
	SystemPrompt string
	Model        string // optional override; empty = use the request's
	ToolNames    []string
}

// ResolveAgent picks the agent definition from the registry snapshot.
// An empty name uses the minimal wiring prompt (no hardcoded agents).
// Unknown names return a typed error listing configured agents.
func ResolveAgent(snap registry.Snapshot, name string) (AgentDef, error) {
	if name == "" {
		return AgentDef{
			Name:         "default",
			SystemPrompt: "You are Karta, a local-first assistant. Work toward the user's goal using the available tools. Be direct and useful.",
		}, nil
	}
	spec, ok := snap.Agents[name]
	if !ok {
		configured := make([]string, 0, len(snap.Agents))
		for n := range snap.Agents {
			configured = append(configured, n)
		}
		return AgentDef{}, fmt.Errorf("engine: unknown agent %q (configured: %s)", name, strings.Join(configured, ", "))
	}
	def := AgentDef{Name: spec.Name, SystemPrompt: spec.SystemPrompt, Model: spec.Model, ToolNames: spec.Tools}
	return def, nil
}

// FilterTools returns only the tools whose names appear in allowlist
// (empty allowlist = all tools).
func FilterTools(p tools.Provider, allowlist []string) []tools.Tool {
	if len(allowlist) == 0 {
		return p.Tools()
	}
	allowed := make(map[string]bool, len(allowlist))
	for _, n := range allowlist {
		allowed[n] = true
	}
	var out []tools.Tool
	for _, t := range p.Tools() {
		if allowed[t.Def().Name] {
			out = append(out, t)
		}
	}
	return out
}

// ExecuteTool runs one tool by name (unknown tools return a typed error
// result the model can see).
func ExecuteTool(ctx context.Context, p tools.Provider, name, argsJSON string) tools.Result {
	return tools.Execute(ctx, p, name, argsJSON)
}
