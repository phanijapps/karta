package resourceindex

import (
	"context"
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
	page, err := ix.Sys.History(context.Background(), domain.HistoryRequest{
		Kinds:      []domain.MemoryKind{KindSkill, KindAgent, KindMCP},
		Tags:       []string{indexTag},
		ActiveOnly: true,
		Limit:      512,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return page.Items
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
