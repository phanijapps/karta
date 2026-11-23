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

// batchLimit mirrors chitta's remember-batch cap; writes are chunked.
const batchLimit = 64

// pageLimit stays within chitta's History limit clamp (limits above
// RecallMaxLimit fall back to the default 10 — the pagination trap the
// phase-2 gate caught).
const pageLimit = 100

// Sync reconciles the SKILL and AGENT parts of the index to the
// registry snapshot. MCP tool records are reconciled ONLY by
// IndexTools (the registry does not know probed tools; a Sync here
// must never retire them). New and changed resources are
// remembered/revised; removed resources are superseded with
// tombstones (chitta has no hard delete); an already-tombstoned record
// is left alone, so repeated syncs are true no-ops.
func (ix *Indexer) Sync(ctx context.Context, snap registry.Snapshot) error {
	want := wantEntries(snap)
	have, err := ix.activeResourceRecords(ctx, []domain.MemoryKind{KindSkill, KindAgent})
	if err != nil {
		return fmt.Errorf("resourceindex: list: %w", err)
	}
	haveBySubject := map[string]domain.MemoryRecord{}
	for _, r := range have {
		haveBySubject[r.Subject] = r
	}
	wantBySubject := map[string]entry{}
	for _, e := range want {
		wantBySubject[e.subject] = e
	}
	var observations []domain.Observation
	for subject, rec := range haveBySubject {
		if strings.HasPrefix(rec.Content, tombstonePrefix) {
			continue // already retired: never revise a tombstone again
		}
		e, still := wantBySubject[subject]
		if still && rec.Content == e.content() {
			continue // unchanged
		}
		if still {
			// changed: supersede the stale content with the new one
			if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{Supersedes: rec.ID, Content: e.content(), Tags: []string{indexTag}}, ix.Host); err != nil {
				return fmt.Errorf("resourceindex: revise %s: %w", subject, err)
			}
			continue
		}
		// removed: tombstone-supersede
		if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{
			Supersedes: rec.ID, Content: tombstonePrefix + subject, Tags: []string{indexTag, "retired"},
		}, ix.Host); err != nil {
			return fmt.Errorf("resourceindex: retire %s: %w", subject, err)
		}
	}
	for _, e := range want {
		rec, exists := haveBySubject[e.subject]
		if !exists {
			observations = append(observations, domain.Observation{
				Kind: e.kind, Subject: e.subject, Content: e.content(), Tags: []string{indexTag},
			})
			continue
		}
		if !strings.HasPrefix(rec.Content, tombstonePrefix) {
			continue // handled above (unchanged or revised)
		}
		// REVIVAL: the subject returned while its tombstone is active.
		// chitta ignores replayed content whose record is superseded,
		// so revival must revise the tombstone back to live content.
		if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{Supersedes: rec.ID, Content: e.content(), Tags: []string{indexTag}}, ix.Host); err != nil {
			return fmt.Errorf("resourceindex: revive %s: %w", e.subject, err)
		}
	}
	return ix.rememberBatched(ctx, observations)
}

// rememberBatched writes observations in chitta-sized batches (a
// single oversized Remember call is rejected outright).
func (ix *Indexer) rememberBatched(ctx context.Context, observations []domain.Observation) error {
	for len(observations) > 0 {
		n := len(observations)
		if n > batchLimit {
			n = batchLimit
		}
		if _, err := ix.Sys.Remember(ctx, observations[:n], ix.Host); err != nil {
			return fmt.Errorf("resourceindex: remember: %w", err)
		}
		observations = observations[n:]
	}
	return nil
}

// ToolInfo is one probed MCP tool (name + description), as the prober
// reports it.
type ToolInfo struct {
	Name        string
	Description string
}

// IndexTools reconciles one server's probed tool list: new tools are
// remembered, changed descriptions supersede the stale actives, tools
// missing from the probe are tombstoned, and an EMPTY probe retires
// the whole server (a disabled server probes nothing). Re-probing
// identical content is a no-op.
func (ix *Indexer) IndexTools(ctx context.Context, server string, tools []ToolInfo) error {
	prefix := fmt.Sprintf("%smcp:%s/", SubjectPfx, server)
	have, err := ix.activeResourceRecords(ctx, []domain.MemoryKind{KindMCP})
	if err != nil {
		return fmt.Errorf("resourceindex: list tools: %w", err)
	}
	wantBySubject := map[string]string{}
	for _, t := range tools {
		wantBySubject[prefix+t.Name] = t.Name + " — " + t.Description
	}
	haveBySubject := map[string]domain.MemoryRecord{}
	for _, rec := range have {
		haveBySubject[rec.Subject] = rec
	}
	var observations []domain.Observation
	for subject, rec := range haveBySubject {
		if !strings.HasPrefix(subject, prefix) {
			continue // another server's tool
		}
		want, still := wantBySubject[subject]
		if strings.HasPrefix(rec.Content, tombstonePrefix) {
			if still {
				// REVIVAL: the tool returned after retirement. chitta
				// ignores replayed content whose record is superseded,
				// so revival revises the tombstone.
				if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{Supersedes: rec.ID, Content: want, Tags: []string{indexTag}}, ix.Host); err != nil {
					return fmt.Errorf("resourceindex: revive tool %s: %w", subject, err)
				}
			}
			continue
		}
		if still && want == rec.Content {
			continue // unchanged
		}
		if still {
			if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{Supersedes: rec.ID, Content: want, Tags: []string{indexTag}}, ix.Host); err != nil {
				return fmt.Errorf("resourceindex: revise tool %s: %w", subject, err)
			}
			continue
		}
		if _, err := ix.Sys.Revise(ctx, domain.ReviseRequest{
			Supersedes: rec.ID, Content: tombstonePrefix + subject, Tags: []string{indexTag, "retired"},
		}, ix.Host); err != nil {
			return fmt.Errorf("resourceindex: retire tool %s: %w", subject, err)
		}
	}
	for subject, content := range wantBySubject {
		if _, exists := haveBySubject[subject]; exists {
			continue // handled above (unchanged, revised, or revived)
		}
		observations = append(observations, domain.Observation{
			Kind: KindMCP, Subject: subject, Content: content, Tags: []string{indexTag},
		})
	}
	return ix.rememberBatched(ctx, observations)
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

// activeResourceRecords lists the index's active records for the sync
// diff, cursor-walking the WHOLE set at chitta's supported page size
// (a limit above the clamp silently shrinks to the default — the
// pagination trap the phase-2 gate caught).
func (ix *Indexer) activeResourceRecords(ctx context.Context, kinds []domain.MemoryKind) ([]domain.MemoryRecord, error) {
	var all []domain.MemoryRecord
	cursor := ""
	for {
		req := domain.HistoryRequest{
			Kinds:      kinds,
			Tags:       []string{indexTag},
			ActiveOnly: true,
			Limit:      pageLimit,
		}
		if cursor != "" {
			req.Cursor = cursor
		}
		page, err := ix.Sys.History(ctx, req)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" || len(page.Items) == 0 {
			return all, nil
		}
		cursor = page.NextCursor
	}
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
