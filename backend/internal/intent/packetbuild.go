package intent

// The wrapper's packet builder (run-legibility split, LOC budget): the
// live roster ranked by recall, capped per kind, system agents excluded.
// The packet's shape lives in packet.go.

import (
	"context"
	"strings"

	"zbot/backend/internal/registry"
)

// buildPacket assembles the analyzer's context packet: the FULL host
// roster (registry snapshot: skills/agents/MCPs with descriptions;
// ward vault: names + canonical scope) ranked by the semantic recall
// (the recall's per-kind hit order — host metadata is the authority,
// memory only ORDERS) and capped per kind. System agents are not
// catalogued — the analyst cannot recommend itself.
func (w *Wrapper) buildPacket(ctx context.Context, goal string) Packet {
	// Recall order per kind: "name — ..." lines, relevance-ordered.
	rank := map[string]map[string]int{
		"skill": {}, "agent": {}, "mcp": {}, "ward": {},
	}
	if w.Cfg.RecallResources != nil {
		res := w.Cfg.RecallResources(ctx, goal)
		for kind, lines := range map[string][]string{
			"skill": res.Skills, "agent": res.Agents, "mcp": res.MCPs, "ward": res.Wards,
		} {
			for i, line := range lines {
				name, _, _ := strings.Cut(line, " — ")
				name = strings.TrimSpace(name)
				if name != "" {
					if _, dup := rank[kind][name]; !dup {
						rank[kind][name] = i
					}
				}
			}
		}
	}
	rankFn := func(kind, id string) int {
		if r, ok := rank[kind][id]; ok {
			return r
		}
		return 1 << 30 // unrecalled: after every recalled entry
	}

	snap := w.snapshot()
	roster := Packet{}
	for name, s := range snap.Skills {
		roster.Skills = append(roster.Skills, CatalogEntry{ID: name, Description: s.Description})
	}
	for name, a := range snap.Agents {
		if registry.IsSystemAgent(name) {
			continue
		}
		roster.Agents = append(roster.Agents, CatalogEntry{ID: name, Description: a.Description})
	}
	for name := range snap.MCPs {
		roster.MCPs = append(roster.MCPs, CatalogEntry{ID: name})
	}
	if w.Wards != nil {
		for _, ward := range w.Wards() {
			roster.Wards = append(roster.Wards, CatalogEntry{ID: ward.Name, Description: ward.Scope})
		}
	}
	return roster.Ranked(rankFn)
}
