package chitta

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"zbot/backend/internal/domain"
)

// ladybugE2E runs only against the real driver (CHITTA_DRIVER=ladybug);
// the shared suite covers semantics for both drivers.
func ladybugE2E(t *testing.T) {
	if os.Getenv("CHITTA_DRIVER") != "ladybug" {
		t.Skip("set CHITTA_DRIVER=ladybug to run the LadybugDB driver E2E suite")
	}
}

func TestLadybugRestartPersistence(t *testing.T) {
	ladybugE2E(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "chitta")

	sys, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	host := agentHost()
	results, err := sys.Remember(ctx, []domain.Observation{{Kind: domain.MemoryKindFact, Content: "survives restarts"}}, host)
	if err != nil {
		t.Fatal(err)
	}
	recID := results[0].Record.ID
	rel, err := sys.Connect(ctx, domain.ConnectRequest{From: "alice", To: "zbot", Type: "works_on"}, host)
	if err != nil {
		t.Fatal(err)
	}
	_ = sys.Close()

	// Reopen the same database: everything intact (spec "Memory survives
	// daemon restarts").
	sys2, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = sys2.Close() }()
	rec, err := sys2.GetRecord(ctx, recID)
	if err != nil {
		t.Fatalf("record lost after restart: %v", err)
	}
	if rec.Content != "survives restarts" {
		t.Fatalf("content corrupted: %q", rec.Content)
	}
	if _, err := sys2.GetRecord(ctx, recID); err != nil {
		t.Fatal(err)
	}
	rel2, found, err := sys2.store.FindRelationship(ctx, rel.From, rel.To, rel.Type)
	if err != nil || !found {
		t.Fatalf("relationship lost after restart: found=%v err=%v", found, err)
	}
	if rel2.ID != rel.ID {
		t.Fatalf("relationship id changed: %s vs %s", rel2.ID, rel.ID)
	}
}

func TestLadybugDimensionMismatchFailsLoudly(t *testing.T) {
	ladybugE2E(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "chitta")
	sys, err := Open(dir, Options{EmbeddingDim: 128})
	if err != nil {
		t.Fatal(err)
	}
	_ = sys.Close()
	// Reopening with a different dimension is a typed unavailable error —
	// never a silent re-index (spec "Dedicated chitta database").
	_, err = Open(dir, Options{EmbeddingDim: 256})
	if err == nil {
		t.Fatal("dimension mismatch must fail")
	}
	var ce *domain.ChittaError
	ok := errors.As(err, &ce)
	if !ok || ce.Code != domain.ErrUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestLadybugFreshOpenCreatesOnlyChittaDir(t *testing.T) {
	ladybugE2E(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "chitta")
	sys, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sys.Close() }()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "chitta" {
		t.Fatalf("fresh open must create only the chitta directory: %v", entries)
	}
}

func TestLadybugConcurrentWrites(t *testing.T) {
	ladybugE2E(t)
	sys, cleanup, err := openForTest(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			host := domain.ChittaHostContext{Source: domain.SourceAgent, SessionID: sessionName(n), AgentID: "root"}
			_, err := sys.Remember(ctx, []domain.Observation{
				{Kind: domain.MemoryKindObservation, Content: "concurrent note " + sessionName(n)},
			}, host)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
	// All distinct-evidence records persist.
	page, err := sys.History(ctx, domain.HistoryRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != writers {
		t.Fatalf("want %d persisted records, got %d", writers, len(page.Items))
	}
}

func sessionName(n int) string {
	return "s-" + string(rune('a'+n))
}

// openForTest builds a System over a temp-dir LadybugDB (used when
// CHITTA_DRIVER=ladybug to run the shared suite against the real driver).
func openForTest(bus domain.EventBus) (*System, func(), error) {
	parent, err := os.MkdirTemp("", "chitta-ladybug-*")
	if err != nil {
		return nil, nil, err
	}
	sys, err := Open(filepath.Join(parent, "chitta"), Options{Bus: bus})
	if err != nil {
		_ = os.RemoveAll(parent)
		return nil, nil, err
	}
	return sys, func() {
		_ = sys.Close()
		_ = os.RemoveAll(parent)
	}, nil
}

// Review round-1 regressions on the real driver: sparse-filter
// pagination over >100 rows must terminate and be exact; timestamp ties
// must walk exactly; resolve-by-id works on Ladybug ids too.
func TestLadybugSparsePaginationOverHundredRows(t *testing.T) {
	ladybugE2E(t)
	sys, cleanup, err := openForTest(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	// 120 records; only every 10th carries the "sparse" tag.
	for i := 0; i < 120; i++ {
		obs := domain.Observation{Kind: domain.MemoryKindObservation, Content: fmt.Sprintf("bulk note %d", i)}
		if i%10 == 0 {
			obs.Tags = []string{"sparse"}
		}
		if _, err := sys.Remember(ctx, []domain.Observation{obs}, agentHost()); err != nil {
			t.Fatal(err)
		}
	}
	// Walk with the sparse tag filter, page size 2 — the loop-fetch must
	// terminate and return each of the 12 matching records exactly once.
	var ids []string
	cursor := ""
	for {
		req := domain.HistoryRequest{Tags: []string{"sparse"}, Limit: 2}
		if cursor != "" {
			req.Cursor = cursor
		}
		page, err := sys.History(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, pageOfIDs(page)...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(ids) != 12 {
		t.Fatalf("want 12 sparse records, walked %d", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id in sparse walk: %s", id)
		}
		seen[id] = true
	}
}

func pageOfIDs(p domain.Page[domain.MemoryRecord]) []string {
	ids := make([]string, 0, len(p.Items))
	for _, r := range p.Items {
		ids = append(ids, r.ID)
	}
	return ids
}

// Review round-3 regressions (real driver): equal-timestamp pagination,
// merge link dedup, Unicode/whitespace recall.
func TestLadybugEqualTimestampPagination(t *testing.T) {
	ladybugE2E(t)
	sys, cleanup, err := openForTest(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	// One batch: all records share one timestamp; walk page size 3.
	obs := make([]domain.Observation, 0, 9)
	for i := 0; i < 9; i++ {
		obs = append(obs, domain.Observation{Kind: domain.MemoryKindObservation, Content: "same-instant " + fmt.Sprint(i)})
	}
	if _, err := sys.Remember(ctx, obs, agentHost()); err != nil {
		t.Fatal(err)
	}
	var ids []string
	cursor := ""
	for {
		req := domain.HistoryRequest{Limit: 3}
		if cursor != "" {
			req.Cursor = cursor
		}
		page, err := sys.History(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, pageOfIDs(page)...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(ids) != 9 {
		t.Fatalf("equal-timestamp walk: want 9, got %d", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate in equal-timestamp walk: %s", id)
		}
		seen[id] = true
	}
}

func TestLadybugMergeLinkDedup(t *testing.T) {
	ladybugE2E(t)
	sys, cleanup, err := openForTest(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	if _, err := sys.UpsertEntity(ctx, domain.EntityUpsert{Name: "postgres", Type: domain.EntityTool}); err != nil {
		t.Fatal(err)
	}
	// Same content twice (distinct sessions), same entity reference: one
	// record, ONE link — no duplicates.
	for _, s := range []string{"s1", "s2", "s3"} {
		_, err := sys.Remember(ctx, []domain.Observation{
			{Kind: domain.MemoryKindFact, Content: "stores data in postgres", Entities: []string{"postgres"}},
		}, domain.ChittaHostContext{Source: domain.SourceAgent, SessionID: s, ExecutionID: s})
		if err != nil {
			t.Fatal(err)
		}
	}
	recs, _ := sys.store.ListRecords(ctx, domain.HistoryRequest{Limit: 10})
	if len(recs.Items) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs.Items))
	}
	ents := recs.Items[0].Entities
	if len(ents) != 1 {
		t.Fatalf("entity links must be deduplicated: %+v", ents)
	}
}

func TestLadybugNormalizedRecall(t *testing.T) {
	ladybugE2E(t)
	sys, cleanup, err := openForTest(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	if _, err := sys.Remember(ctx, []domain.Observation{
		{Kind: domain.MemoryKindFact, Content: "Café   au lait preference"},
	}, agentHost()); err != nil {
		t.Fatal(err)
	}
	// Whitespace-variant, NFC-equivalent query must hit via the stored
	// normalized_content (review round 3: searching lower(content) missed).
	page, err := sys.Recall(ctx, domain.RecallRequest{Query: "café au  lait"}, domain.ChittaHostContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatal("normalized recall must match whitespace/Unicode variants")
	}
}

// Schema v2 migration: a v1-shaped database (DOUBLE[] embeddings,
// schema_version=1) opens, migrates once, and stays stable.
func TestLadybugV1toV2Migration(t *testing.T) {
	ladybugE2E(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "chitta")
	sys, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := sys.Remember(ctx, []domain.Observation{{Kind: domain.MemoryKindFact, Content: "survives migration"}}, agentHost()); err != nil {
		t.Fatal(err)
	}
	recID := ""
	if recs, _ := sys.store.ListRecords(ctx, domain.HistoryRequest{Limit: 10}); len(recs.Items) == 1 {
		recID = recs.Items[0].ID
	}
	if recID == "" {
		t.Fatal("seed record missing")
	}
	_ = sys.Close()

	// Downgrade the schema marker to v1 (and the column is already v2 —
	// the migration only rebuilds when the version says 1; simulate the
	// full v1 shape by writing the marker directly).
	sys2, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sys2.store.(interface {
		DebugQuery(string, map[string]any) ([]map[string]any, error)
	}).DebugQuery("MATCH (m:ChittaMeta {key: 'schema_version'}) SET m.value = '1'", nil); err != nil {
		t.Fatal(err)
	}
	_ = sys2.Close()

	sys3, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopen after v1 marking: %v", err)
	}
	defer func() { _ = sys3.Close() }()
	rec, err := sys3.GetRecord(ctx, recID)
	if err != nil {
		t.Fatalf("record lost in migration: %v", err)
	}
	if rec.Content != "survives migration" {
		t.Fatalf("content corrupted: %q", rec.Content)
	}
}
