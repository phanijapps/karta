package recalleval

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"zbot/backend/chitta"
	"zbot/backend/internal/domain"
)

func TestGoldenFloors(t *testing.T) {
	t.Parallel()
	suite, err := Load("fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(suite)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("nDCG@10=%.3f MRR@10=%.3f", res.NDCG10, res.MRR10)
	// Happy-path score pin (F-20 evidence: the fail-closed fix leaves the
	// deterministic golden scores unchanged). Baseline measured pre-fix:
	// nDCG@10=0.829 MRR@10=0.762 — pinned with a small epsilon floor.
	if res.NDCG10 < 0.82 {
		t.Fatalf("golden nDCG@10 regressed: %.3f (pin >= 0.82)", res.NDCG10)
	}
	if res.MRR10 < 0.75 {
		t.Fatalf("golden MRR@10 regressed: %.3f (pin >= 0.75)", res.MRR10)
	}
}

func TestRegressionTripsFloor(t *testing.T) {
	t.Parallel()
	suite, err := Load("fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Simulated regression: a corrupted query that matches nothing —
	// its floor must trip (spec "Regression trips the floor").
	for i := range suite.Cases {
		suite.Cases[i].Query = "zzqqxx nothing matches this"
		suite.Cases[i].Floor = 0.5
	}
	if _, err := Run(suite); err == nil {
		t.Fatal("floor violation must fail the run")
	}
}

// --- F-20: embedding backfill fails closed; vector lane participates -----

// failingBackfillSys poisons exactly one stage of the backfill.
type failingBackfillSys struct {
	failFetch bool
	failSave  bool
	fetched   int
	saved     [][]domain.RecordEmbedding
}

func (f *failingBackfillSys) RecordsAwaitingEmbedding(_ context.Context, _ int) ([]string, []string, error) {
	if f.failFetch {
		return nil, nil, errors.New("db exploded")
	}
	f.fetched++
	return []string{"r1", "r2"}, []string{"alpha", "beta"}, nil
}

func (f *failingBackfillSys) SetRecordEmbeddings(_ context.Context, e []domain.RecordEmbedding) error {
	if f.failSave {
		return errors.New("disk full")
	}
	f.saved = append(f.saved, e)
	return nil
}

type errEmbedder struct{}

func (errEmbedder) EmbedQuery(context.Context, string) ([]float64, error) { return nil, nil }
func (errEmbedder) EmbedPassages(context.Context, []string) ([][]float64, error) {
	return nil, errors.New("embedder on fire")
}
func (errEmbedder) Close() error { return nil }

type countingEmbedder struct {
	inner    chitta.Embedder
	queries  int
	passages int
	vectors  int // passages for which EmbedPassages returned non-empty vectors
}

func (c *countingEmbedder) EmbedQuery(ctx context.Context, q string) ([]float64, error) {
	c.queries++
	return c.inner.EmbedQuery(ctx, q)
}

func (c *countingEmbedder) EmbedPassages(ctx context.Context, t []string) ([][]float64, error) {
	c.passages++
	vecs, err := c.inner.EmbedPassages(ctx, t)
	if err == nil && len(vecs) == len(t) {
		for _, v := range vecs {
			if len(v) > 0 {
				c.vectors++
			}
		}
	}
	return vecs, err
}

func (c *countingEmbedder) Close() error { return nil }

func TestBackfillFailsClosedAllThreeStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ok := &deterministicEmbedder{}

	// Stage 1: fetch fails.
	err := backfillEmbeddings(ctx, &failingBackfillSys{failFetch: true}, ok)
	if err == nil || !strings.Contains(err.Error(), "backfill (fetch)") {
		t.Fatalf("fetch failure must propagate, got %v", err)
	}
	// Stage 2: embed fails.
	err = backfillEmbeddings(ctx, &failingBackfillSys{}, errEmbedder{})
	if err == nil || !strings.Contains(err.Error(), "backfill (embed)") {
		t.Fatalf("embed failure must propagate, got %v", err)
	}
	// Stage 3: save fails.
	err = backfillEmbeddings(ctx, &failingBackfillSys{failSave: true}, ok)
	if err == nil || !strings.Contains(err.Error(), "backfill (save)") {
		t.Fatalf("save failure must propagate, got %v", err)
	}
}

func TestBackfillHappyPathStoresVectors(t *testing.T) {
	t.Parallel()
	sys := &failingBackfillSys{}
	if err := backfillEmbeddings(context.Background(), sys, &deterministicEmbedder{}); err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if len(sys.saved) != 1 || len(sys.saved[0]) != 2 {
		t.Fatalf("expected 2 stored embeddings, got %+v", sys.saved)
	}
}

// TestVectorLaneParticipates proves the eval actually exercises the
// vector lane: the embedder is consulted at query time (EmbedQuery) and
// at seed time (EmbedPassages) during a real Run.
func TestVectorLaneParticipates(t *testing.T) {
	t.Parallel()
	counting := &countingEmbedder{inner: &deterministicEmbedder{}}
	suite, err := Load("fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWithEmbedder(suite, counting); err != nil {
		t.Fatalf("golden run must pass with the counting embedder: %v", err)
	}
	if counting.passages == 0 {
		t.Fatal("vector lane did not participate: EmbedPassages never called (seed embeddings skipped)")
	}
	if counting.queries == 0 {
		t.Fatal("vector lane did not participate: EmbedQuery never called during Recall")
	}
	if counting.vectors == 0 {
		t.Fatal("vector lane did not participate: EmbedPassages produced no usable vectors")
	}
}

// TestRunPropagatesBackfillFailure proves the WIRING, not just the
// helper: a poisoned embedder makes the real Run return the wrapped
// error (F-20 fail-closed reaches the caller).
func TestRunPropagatesBackfillFailure(t *testing.T) {
	t.Parallel()
	suite, err := Load("fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = runWithEmbedder(suite, errEmbedder{})
	if err == nil || !strings.Contains(err.Error(), "backfill (embed)") {
		t.Fatalf("Run must propagate the poisoned backfill error, got %v", err)
	}
}

// garbageEmbedder poisons BOTH lanes' vector content while staying
// error-free: queries get one constant vector; passages get distinct but
// content-independent vectors.
type garbageEmbedder struct{}

func (g garbageEmbedder) EmbedQuery(_ context.Context, _ string) ([]float64, error) {
	v := make([]float64, 384)
	v[0] = 1
	return v, nil
}
func (g garbageEmbedder) EmbedPassages(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		v := make([]float64, 384)
		v[i%384] = 1
		out[i] = v
	}
	return out, nil
}
func (g garbageEmbedder) Close() error { return nil }

// TestGoldenFloorsAreVectorInsensitive CHARACTERIZES finding F-30: with
// garbage embeddings on both the query and passage side, the golden
// floors still pass with IDENTICAL scores (nDCG 0.829 / MRR 0.762) —
// BM25 dominates these fixtures so completely that the eval cannot
// detect vector-lane regressions, despite the harness comment claiming
// the lane is exercised. The lane RUNS (TestVectorLaneParticipates
// proves embedder calls at query time) but is not CAUSAL here. Flip this
// test when the follow-up recalibrates fixtures/fusion so vector
// quality affects outcomes.
func TestGoldenFloorsAreVectorInsensitive(t *testing.T) {
	t.Parallel()
	suite, err := Load("fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runWithEmbedder(suite, garbageEmbedder{})
	if err != nil {
		t.Fatalf("garbage embeddings tripped the floors — vector sensitivity arrived (F-30 resolved): %v", err)
	}
	if math.Abs(res.NDCG10-0.829) > 5e-4 || math.Abs(res.MRR10-0.762) > 5e-4 {
		t.Fatalf("garbage-embedding scores drifted from the pinned BM25-dominated baseline: nDCG=%.3f MRR=%.3f", res.NDCG10, res.MRR10)
	}
}
