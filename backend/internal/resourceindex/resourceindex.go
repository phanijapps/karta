// Package resourceindex keeps the semantic resource index for intent
// analysis: skills, agents, and probed MCP tools embedded as chitta
// records of RESERVED kinds, recalled by EmbedQuery of the user goal.
// It is derived data: the registry snapshot (and probe results) are
// the source of truth; the index is rebuilt to match on every sync.
//
// Deletion semantics: chitta has no hard delete — removed resources
// are SUPERSEDED with tombstone records (content prefix
// `retired resource:`), and the recall path post-filters tombstones,
// so retired entries never surface as recommendations.
//
// Boundaries: a thin seam interface over chitta's exported surface
// (the real *chitta.System in production, chitta.OpenInMemory in the
// integration test); imports domain + registry only.
package resourceindex

import (
	"context"
	"fmt"
	"strings"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// Reserved record kinds (defined on domain; the indexer owns them).
// Subjects are stable: resource:skill:<name>, resource:agent:<name>,
// resource:mcp:<server>/<tool>.
const (
	KindSkill  = domain.MemoryKindResourceSkill
	KindAgent  = domain.MemoryKindResourceAgent
	KindMCP    = domain.MemoryKindResourceMCPTool
	SubjectPfx = "resource:"
	// tombstonePrefix marks superseded (removed) resources; recall
	// post-filters these out.
	tombstonePrefix = "retired resource:"
	// indexTag rides every index record for auditing.
	indexTag = "resource-index"
)

// System is the chitta seam the indexer needs (the real System
// satisfies it; OpenInMemory serves tests).
type System interface {
	Remember(ctx context.Context, observations []domain.Observation, host domain.ChittaHostContext) ([]domain.RememberResult, error)
	Revise(ctx context.Context, req domain.ReviseRequest, host domain.ChittaHostContext) (domain.MemoryRecord, error)
	Recall(ctx context.Context, req domain.RecallRequest, host domain.ChittaHostContext) (domain.Page[domain.RecallHit], error)
	History(ctx context.Context, req domain.HistoryRequest) (domain.Page[domain.MemoryRecord], error)
}

// Indexer reconciles the resource index against the registry. Host is
// the trusted-host context for chitta writes (daemon-side machinery:
// the distiller source with AgentID "resource-index" — provenance
// names the true writer).
type Indexer struct {
	Sys  System
	Host domain.ChittaHostContext
}

// Hit is one relevant resource for the intent prompt's slots.
type Hit struct {
	Kind        domain.MemoryKind
	Name        string
	Description string
}

// Line renders the hit for the prompt list ("- name — description").
func (h Hit) Line() string {
	if h.Description == "" {
		return h.Name
	}
	return h.Name + " — " + h.Description
}

// entry is the internal want-state for one resource.
type entry struct {
	kind        domain.MemoryKind
	subject     string
	name        string
	description string
}

func (e entry) content() string { return e.name + " — " + e.description }

// Sync reconciles the whole index to the registry snapshot: new and
// changed resources are remembered (content-keyed merges bump
// unchanged ones), removed resources are superseded with tombstones.
func (ix *Indexer) Sync(ctx context.Context, snap registry.Snapshot) error {
	want := wantEntries(snap)
	have, err := ix.activeResourceRecords(ctx)
	if err != nil {
		return fmt.Errorf("resourceindex: list: %w", err)
	}
	// Diff by subject.
	haveBySubject := map[string]domain.MemoryRecord{}
	for _, r := range have {
		haveBySubject[r.Subject] = r
	}
	wantBySubject := map[string]entry{}
	for _, e := range want {
		wantBySubject[e.subject] = e
	}
	var observations []domain.Observation
	for _, e := range want {
		rec, exists := haveBySubject[e.subject]
		if exists && rec.Content == e.content() {
			continue // unchanged
		}
		if exists {
			// changed: supersede the stale content with the new one
			if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{Supersedes: rec.ID, Content: e.content(), Tags: []string{indexTag}}, ix.Host); err != nil {
				return fmt.Errorf("resourceindex: revise %s: %w", e.subject, err)
			}
			continue
		}
		observations = append(observations, domain.Observation{
			Kind: e.kind, Subject: e.subject, Content: e.content(), Tags: []string{indexTag},
		})
	}
	for subject, rec := range haveBySubject {
		if _, still := wantBySubject[subject]; still {
			continue
		}
		// removed: tombstone-supersede (chitta has no hard delete)
		if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{
			Supersedes: rec.ID, Content: tombstonePrefix + subject, Tags: []string{indexTag, "retired"},
		}, ix.Host); err != nil {
			return fmt.Errorf("resourceindex: retire %s: %w", subject, err)
		}
	}
	if len(observations) > 0 {
		if _, err := ix.Sys.Remember(ctx, observations, ix.Host); err != nil {
			return fmt.Errorf("resourceindex: remember: %w", err)
		}
	}
	return nil
}

// ToolInfo is one probed MCP tool (name + description), as the prober
// reports it.
type ToolInfo struct {
	Name        string
	Description string
}

// IndexTools upserts one enabled server's probed tool list (the prober
// output is not persisted anywhere else; probes feed the index).
// Tools that vanish from a later probe are tombstoned by the next Sync
// pass over the same subjects.
func (ix *Indexer) IndexTools(ctx context.Context, server string, tools []ToolInfo) error {
	var observations []domain.Observation
	for _, t := range tools {
		subject := fmt.Sprintf("%smcp:%s/%s", SubjectPfx, server, t.Name)
		observations = append(observations, domain.Observation{
			Kind: KindMCP, Subject: subject,
			Content: t.Name + " — " + t.Description, Tags: []string{indexTag},
		})
	}
	if len(observations) == 0 {
		return nil
	}
	if _, err := ix.Sys.Remember(ctx, observations, ix.Host); err != nil {
		return fmt.Errorf("resourceindex: index tools: %w", err)
	}
	return nil
}

// Relevant recalls the resource kinds for a goal (semantic lanes; the
// vector lane participates when the embedder is warm). Tombstones are
// post-filtered so retired resources never recommend.
func (ix *Indexer) Relevant(ctx context.Context, goal string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 12
	}
	page, err := ix.Sys.Recall(ctx, domain.RecallRequest{
		Query: goal, Kinds: []domain.MemoryKind{KindSkill, KindAgent, KindMCP}, Limit: limit,
	}, ix.Host)
	if err != nil {
		return nil, fmt.Errorf("resourceindex: recall: %w", err)
	}
	var hits []Hit
	for _, h := range page.Items {
		content := h.Record.Content
		if strings.HasPrefix(content, tombstonePrefix) {
			continue
		}
		name, desc, _ := strings.Cut(content, " — ")
		if name == "" {
			continue
		}
		hits = append(hits, Hit{Kind: h.Record.Kind, Name: name, Description: desc})
	}
	return hits, nil
}

// activeResourceRecords lists the index's active records (all three
// reserved kinds, tagged) for the sync diff.
func (ix *Indexer) activeResourceRecords(ctx context.Context) ([]domain.MemoryRecord, error) {
	page, err := ix.Sys.History(ctx, domain.HistoryRequest{
		Kinds:      []domain.MemoryKind{KindSkill, KindAgent, KindMCP},
		Tags:       []string{indexTag},
		ActiveOnly: true,
		Limit:      512,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// wantEntries derives the want-state from the registry snapshot:
// skills (name+description) and agents (description only — agents
// without one are not delegatable and not offered).
func wantEntries(snap registry.Snapshot) []entry {
	var out []entry
	for name, s := range snap.Skills {
		out = append(out, entry{kind: KindSkill, subject: SubjectPfx + "skill:" + name, name: name, description: s.Description})
	}
	for name, a := range snap.Agents {
		if a.Description == "" {
			continue
		}
		out = append(out, entry{kind: KindAgent, subject: SubjectPfx + "agent:" + name, name: name, description: a.Description})
	}
	return out
}
