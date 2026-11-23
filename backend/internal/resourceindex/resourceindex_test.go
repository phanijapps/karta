package resourceindex

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"zbot/backend/chitta"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

func newTestIndexer(t *testing.T) *Indexer {
	t.Helper()
	sys, err := chitta.OpenInMemory(nil)
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	return &Indexer{Sys: sys, Host: domain.ChittaHostContext{Source: domain.SourceDistiller, AgentID: "resource-index"}}
}

func snapOf(skills, agents map[string]string) registry.Snapshot {
	snap := registry.Snapshot{}
	snap.Skills = map[string]registry.SkillSpec{}
	for n, d := range skills {
		snap.Skills[n] = registry.SkillSpec{Name: n, Description: d}
	}
	snap.Agents = map[string]registry.AgentSpec{}
	for n, d := range agents {
		snap.Agents[n] = registry.AgentSpec{Name: n, Description: d}
	}
	return snap
}

// Sync → indexed; removal → tombstoned and never recalled; re-sync is
// a no-op (merge-bump, no duplicate actives).
func TestSyncLifecycle(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()

	snap := snapOf(
		map[string]string{"clean-code": "coding standards", "humanizer": "voice"},
		map[string]string{"research-agent": "gathers evidence", "silent-agent": ""},
	)
	if err := ix.Sync(ctx, snap); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// silent-agent has no description: not indexed.
	hits, err := ix.Relevant(ctx, "coding standards", 12)
	if err != nil {
		t.Fatalf("Relevant: %v", err)
	}
	names := map[string]bool{}
	for _, h := range hits {
		names[h.Name] = true
		if h.Kind == KindAgent && h.Name == "silent-agent" {
			t.Fatal("description-less agent must not index")
		}
	}
	if !names["clean-code"] {
		t.Fatalf("clean-code must recall: %+v", hits)
	}

	// Re-sync unchanged: no duplicate ACTIVE records.
	if err := ix.Sync(ctx, snap); err != nil {
		t.Fatalf("re-Sync: %v", err)
	}
	active := tActive(t, ix)
	if countSubject(active, SubjectPfx+"skill:clean-code") != 1 {
		t.Fatalf("unchanged re-sync must not duplicate: %d", countSubject(active, SubjectPfx+"skill:clean-code"))
	}

	// Removal: humanizer gone → tombstoned, never recalled.
	changed := snapOf(
		map[string]string{"clean-code": "coding standards v2"}, // changed description
		map[string]string{"research-agent": "gathers evidence"},
	)
	if err := ix.Sync(ctx, changed); err != nil {
		t.Fatalf("Sync removal: %v", err)
	}
	hits, _ = ix.Relevant(ctx, "humanizer", 12)
	for _, h := range hits {
		if h.Name == "humanizer" {
			t.Fatal("removed resource must not recall")
		}
	}
	// Changed description superseded cleanly: one active with new content.
	active = tActive(t, ix)
	n := countSubject(active, SubjectPfx+"skill:clean-code")
	if n != 1 {
		t.Fatalf("changed resource must have exactly one active, got %d", n)
	}
	for _, r := range active {
		if r.Subject == SubjectPfx+"skill:clean-code" && r.Content != "clean-code — coding standards v2" {
			t.Fatalf("stale content active: %q", r.Content)
		}
	}
}

func TestIndexToolsAndRecall(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()
	if err := ix.IndexTools(ctx, "search-mcp", []ToolInfo{
		{Name: "web_search", Description: "search the web"},
		{Name: "read_page", Description: "read a url as markdown"},
	}); err != nil {
		t.Fatalf("IndexTools: %v", err)
	}
	hits, err := ix.Relevant(ctx, "search the web", 12)
	if err != nil {
		t.Fatalf("Relevant: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Name == "web_search" && h.Kind == KindMCP {
			found = true
		}
	}
	if !found {
		t.Fatalf("web_search must recall: %+v", hits)
	}
}

func TestHitLine(t *testing.T) {
	if got := (Hit{Name: "x", Description: "y"}).Line(); got != "x — y" {
		t.Fatalf("line: %q", got)
	}
	if got := (Hit{Name: "x"}).Line(); got != "x" {
		t.Fatalf("bare line: %q", got)
	}
}

func tActive(t *testing.T, ix *Indexer) []domain.MemoryRecord {
	t.Helper()
	// Use the indexer's own cursor-walking list (a raw History limit of
	// 512 would clamp to 10 — the very trap under test). Tombstones
	// share the subject and stay ACTIVE — count LIVE records only.
	recs, err := ix.activeResourceRecords(context.Background(), []domain.MemoryKind{KindSkill, KindAgent, KindMCP})
	if err != nil {
		t.Fatalf("activeResourceRecords: %v", err)
	}
	var live []domain.MemoryRecord
	for _, r := range recs {
		if !strings.HasPrefix(r.Content, tombstonePrefix) {
			live = append(live, r)
		}
	}
	return live
}

func countSubject(records []domain.MemoryRecord, subject string) int {
	n := 0
	for _, r := range records {
		if r.Subject == subject {
			n++
		}
	}
	return n
}

var _ = strings.TrimSpace // keep strings if future edits drop its use

// Pagination: reconciliation must see the WHOLE active set — chitta
// clamps History limits above 100 down to 10, the trap the phase-2
// gate caught. Index 30 skills; a changed/removed pair beyond the
// first page must reconcile correctly.
func TestSyncPaginatesActiveSet(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()
	big := map[string]string{}
	for i := 0; i < 30; i++ {
		big[fmt.Sprintf("skill-%02d", i)] = fmt.Sprintf("description %02d", i)
	}
	if err := ix.Sync(ctx, snapOf(big, nil)); err != nil {
		t.Fatalf("Sync big: %v", err)
	}
	// Change skill-25, remove skill-29 — both beyond any first page of 10.
	big["skill-25"] = "description 25 v2"
	delete(big, "skill-29")
	if err := ix.Sync(ctx, snapOf(big, nil)); err != nil {
		t.Fatalf("Sync change: %v", err)
	}
	active := tActive(t, ix)
	if countSubject(active, SubjectPfx+"skill:skill-25") != 1 {
		t.Fatal("changed record beyond page 1 must have exactly one active")
	}
	for _, r := range active {
		if r.Subject == SubjectPfx+"skill:skill-25" && r.Content != "skill-25 — description 25 v2" {
			t.Fatalf("stale content: %q", r.Content)
		}
	}
	if countSubject(tActive(t, ix), SubjectPfx+"skill:skill-29") != 0 {
		t.Fatal("removed record beyond page 1 must be tombstoned (not active)")
	}
}

// Batches: a roster larger than chitta's 64-observation remember cap
// still indexes completely.
func TestSyncBatchesOver64(t *testing.T) {
	ix := newTestIndexer(t)
	big := map[string]string{}
	for i := 0; i < 80; i++ {
		big[fmt.Sprintf("s%02d", i)] = "d"
	}
	if err := ix.Sync(context.Background(), snapOf(big, nil)); err != nil {
		t.Fatalf("Sync 80: %v", err)
	}
	if n := len(tActive(t, ix)); n != 80 {
		t.Fatalf("want 80 active resource records, got %d", n)
	}
}

// Tombstone idempotency: a sync after a removal must not revise the
// tombstone again (no-op re-sync), and a re-added resource returns.
func TestTombstoneIdempotencyAndReadd(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()
	full := snapOf(map[string]string{"a": "da", "b": "db"}, nil)
	if err := ix.Sync(ctx, full); err != nil {
		t.Fatal(err)
	}
	trimmed := snapOf(map[string]string{"a": "da"}, nil)
	if err := ix.Sync(ctx, trimmed); err != nil {
		t.Fatal(err)
	}
	// Re-sync the SAME trimmed snapshot: no new revisions (count active
	// tombstones indirectly: active set stays 1, and no error).
	if err := ix.Sync(ctx, trimmed); err != nil {
		t.Fatal(err)
	}
	if n := len(tActive(t, ix)); n != 1 {
		t.Fatalf("no-op re-sync changed the active set: %d", n)
	}
	// Re-add b: back to 2 actives.
	if err := ix.Sync(ctx, full); err != nil {
		t.Fatal(err)
	}
	if n := len(tActive(t, ix)); n != 2 {
		t.Fatalf("re-added resource missing: %d", n)
	}
}

// MCP lifecycle: changed descriptions supersede; a reduced probe
// retires vanished tools; an empty probe retires the server; and a
// registry Sync NEVER retires indexed tools.
func TestIndexToolsLifecycle(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()
	if err := ix.IndexTools(ctx, "srv", []ToolInfo{
		{Name: "keep", Description: "stays"}, {Name: "change", Description: "old"}, {Name: "drop", Description: "goes"},
	}); err != nil {
		t.Fatal(err)
	}
	// Registry sync in between must not touch MCP records.
	if err := ix.Sync(ctx, snapOf(map[string]string{"skill": "d"}, nil)); err != nil {
		t.Fatal(err)
	}
	if got := countSubject(tActive(t, ix), SubjectPfx+"mcp:srv/keep"); got != 1 {
		t.Fatalf("registry Sync retired an MCP tool: %d", got)
	}
	// Reduced+changed probe: keep stays, change revises, drop retires.
	if err := ix.IndexTools(ctx, "srv", []ToolInfo{
		{Name: "keep", Description: "stays"}, {Name: "change", Description: "new"},
	}); err != nil {
		t.Fatal(err)
	}
	active := tActive(t, ix)
	if countSubject(active, SubjectPfx+"mcp:srv/drop") != 0 {
		t.Fatal("vanished tool must be retired")
	}
	for _, r := range active {
		if r.Subject == SubjectPfx+"mcp:srv/change" && r.Content != "change — new" {
			t.Fatalf("stale tool description: %q", r.Content)
		}
	}
	if countSubject(active, SubjectPfx+"mcp:srv/change") != 1 {
		t.Fatal("changed tool must have exactly one active")
	}
	// Identical re-probe: no-op (still exactly these actives).
	if err := ix.IndexTools(ctx, "srv", []ToolInfo{
		{Name: "keep", Description: "stays"}, {Name: "change", Description: "new"},
	}); err != nil {
		t.Fatal(err)
	}
	// Empty probe retires the server entirely.
	if err := ix.IndexTools(ctx, "srv", nil); err != nil {
		t.Fatal(err)
	}
	if got := countSubject(tActive(t, ix), SubjectPfx+"mcp:srv/keep"); got != 0 {
		t.Fatalf("empty probe must retire the server: %d", got)
	}
	hits, _ := ix.Relevant(ctx, "stays", 12)
	for _, h := range hits {
		if h.Name == "keep" {
			t.Fatal("retired tool must not recall")
		}
	}
}

// A retired tool that returns in a later probe REVIVES (chitta ignores
// replayed content on superseded records — revival must revise the
// tombstone, and identical re-probes must be no-ops).
func TestIndexToolsRevival(t *testing.T) {
	ix := newTestIndexer(t)
	ctx := context.Background()
	probe := []ToolInfo{{Name: "keep", Description: "stays"}}
	if err := ix.IndexTools(ctx, "srv", probe); err != nil {
		t.Fatal(err)
	}
	if err := ix.IndexTools(ctx, "srv", nil); err != nil { // retire all
		t.Fatal(err)
	}
	if n := len(tActive(t, ix)); n != 0 {
		t.Fatalf("server retired: %d live", n)
	}
	if err := ix.IndexTools(ctx, "srv", probe); err != nil { // revive
		t.Fatal(err)
	}
	if n := len(tActive(t, ix)); n != 1 {
		t.Fatalf("revived tool missing: %d live", n)
	}
	hits, _ := ix.Relevant(ctx, "stays", 12)
	if len(hits) != 1 || hits[0].Name != "keep" {
		t.Fatalf("revived tool must recall: %+v", hits)
	}
}
