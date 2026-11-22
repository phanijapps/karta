// Package recalleval is the recall evaluation harness: a golden fixture
// set (queries with expected records and floors), a seeded-database
// runner over the real LadybugDB driver, and nDCG@10 / MRR@10 /
// presence metrics. `task test:chitta:recall` runs it; floors gate
// recall-affecting changes (spec chitta-recall "Evaluation harness with
// floors").
package recalleval

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"zbot/backend/chitta"
	"zbot/backend/internal/domain"
)

// Fixture is one golden query. ExpectIDs are CONTENT SUBSTRINGS
// (case-insensitive), not record ids.
type Fixture struct {
	Query     string   `yaml:"query"`
	ExpectIDs []string `yaml:"expect_ids"`
	Floor     float64  `yaml:"floor"` // min nDCG@10
}

// Suite is the fixture file.
type Suite struct {
	Seed   []SeedRecord `yaml:"seed"`
	Cases  []Fixture    `yaml:"cases"`
	Floors struct {
		NDCG10 float64 `yaml:"ndcg_at_10"`
		MRR10  float64 `yaml:"mrr_at_10"`
	} `yaml:"floors"`
}

// SeedRecord is one record to seed before evaluation.
type SeedRecord struct {
	Kind    string `yaml:"kind"`
	Content string `yaml:"content"`
	Subject string `yaml:"subject,omitempty"`
}

// Load reads a fixture file.
func Load(path string) (*Suite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("recalleval: read %s: %w", path, err)
	}
	var s Suite
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("recalleval: parse %s: %w", path, err)
	}
	return &s, nil
}

// Result carries one run's metrics.
type Result struct {
	NDCG10 float64
	MRR10  float64
}

// deterministicEmbedder produces stable content-derived vectors: shared
// tokens pull records together, so near-duplicate fixtures cluster and
// MMR/vector fusion are actually under test.
type deterministicEmbedder struct{}

func (deterministicEmbedder) EmbedQuery(ctx context.Context, q string) ([]float64, error) {
	return contentVector(q, 0), nil
}
func (deterministicEmbedder) EmbedPassages(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		out = append(out, contentVector(t, 1))
	}
	return out, nil
}
func (deterministicEmbedder) Close() error { return nil }

func contentVector(s string, bias int) []float64 {
	v := make([]float64, 384)
	for _, r := range s {
		v[int(r)%384]++
	}
	v[bias%384] += 0.5
	n := 0.0
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		return v
	}
	for i := range v {
		v[i] /= n
	}
	return v
}

// backfillSystem is the slice of chitta.System the embedding backfill
// needs — an interface so tests can poison each call deterministically.
type backfillSystem interface {
	RecordsAwaitingEmbedding(ctx context.Context, limit int) (ids []string, contents []string, err error)
	SetRecordEmbeddings(ctx context.Context, embeddings []domain.RecordEmbedding) error
}

// backfillEmbeddings stores vectors for every seeded record lacking one.
// All three stages fail closed (F-20): a swallowed error here would let
// the eval pass on BM25 alone while claiming the vector lane ran.
func backfillEmbeddings(ctx context.Context, sys backfillSystem, emb chitta.Embedder) error {
	ids, contents, err := sys.RecordsAwaitingEmbedding(ctx, 256)
	if err != nil {
		return fmt.Errorf("recalleval: embedding backfill (fetch): %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	vecs, err := emb.EmbedPassages(ctx, contents)
	if err != nil {
		return fmt.Errorf("recalleval: embedding backfill (embed): %w", err)
	}
	stored := make([]domain.RecordEmbedding, 0, len(ids))
	for i, id := range ids {
		stored = append(stored, domain.RecordEmbedding{RecordID: id, Embedding: vecs[i]})
	}
	if err := sys.SetRecordEmbeddings(ctx, stored); err != nil {
		return fmt.Errorf("recalleval: embedding backfill (save): %w", err)
	}
	return nil
}

// Run evaluates with the deterministic embedder (the production default).
func Run(suite *Suite) (*Result, error) {
	return runWithEmbedder(suite, &deterministicEmbedder{})
}

// runWithEmbedder is Run with an injectable embedder — tests pass
// counting/garbage/failing doubles WITHOUT global mutable state
// (parallel tests each own their embedder). It seeds a fresh temp
// chitta database and evaluates every fixture.
func runWithEmbedder(suite *Suite, embedder chitta.Embedder) (*Result, error) {
	parent, err := os.MkdirTemp("", "chitta-recalleval-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(parent) }()
	sys, err := chitta.Open(parent+"/chitta", chitta.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = sys.Close() }()
	sys.ConfigureRecall(domain.DefaultRecallSettings())
	// Deterministic embedder keeps the run reproducible. NOTE (F-30):
	// the golden floors are currently VECTOR-INSENSITIVE (BM25 dominates;
	// TestGoldenFloorsAreVectorInsensitive pins this) — the lane provably
	// runs (TestVectorLaneParticipates) but does not yet move the floors;
	// recalibration is the follow-up.
	sys.SetEmbedder(embedder)
	ctx := context.Background() // ctx-ok: eval-harness root (test infrastructure)
	host := domain.ChittaHostContext{Source: domain.SourceAgent, SessionID: "eval", AgentID: "eval"}

	// Embed everything seeded (the vector lane needs stored vectors).
	// Done after seeding via the trusted seam below.
	obs := make([]domain.Observation, 0, len(suite.Seed))
	for _, s := range suite.Seed {
		obs = append(obs, domain.Observation{
			Kind:    domain.MemoryKind(s.Kind),
			Content: s.Content,
			Subject: s.Subject,
		})
	}
	if _, err := sys.Remember(ctx, obs, host); err != nil {
		return nil, fmt.Errorf("recalleval: seed: %w", err)
	}
	// Backfill embeddings through the trusted seam. Every failure is
	// FATAL: silently skipping here would score BM25 only while claiming
	// the vector lane ran (final-gate finding — the harness fails closed).
	if err := backfillEmbeddings(ctx, sys, embedder); err != nil {
		return nil, err
	}

	var ndcgs, mrrs []float64
	for _, fx := range suite.Cases {
		page, err := sys.Recall(ctx, domain.RecallRequest{Query: fx.Query, Limit: 10}, host)
		if err != nil {
			return nil, fmt.Errorf("recalleval: query %q: %w", fx.Query, err)
		}
		ranks := make([]int, 0, len(fx.ExpectIDs)) // 1-based rank per expect
		for _, want := range fx.ExpectIDs {
			for i, h := range page.Items {
				if contains(h.Record.Content, want) {
					ranks = append(ranks, i+1)
					break
				}
			}
		}
		ndcgs = append(ndcgs, ndcg(ranks, len(fx.ExpectIDs)))
		mrrs = append(mrrs, mrr(ranks))
		if fx.Floor > 0 && ndcgs[len(ndcgs)-1] < fx.Floor {
			return nil, fmt.Errorf("recalleval: query %q nDCG@10 %.3f below floor %.3f", fx.Query, ndcgs[len(ndcgs)-1], fx.Floor)
		}
	}
	sort.Float64s(ndcgs)
	sort.Float64s(mrrs)
	res := &Result{NDCG10: mean(ndcgs), MRR10: mean(mrrs)}
	if suite.Floors.NDCG10 > 0 && res.NDCG10 < suite.Floors.NDCG10 {
		return nil, fmt.Errorf("recalleval: aggregate nDCG@10 %.3f below floor %.3f", res.NDCG10, suite.Floors.NDCG10)
	}
	if suite.Floors.MRR10 > 0 && res.MRR10 < suite.Floors.MRR10 {
		return nil, fmt.Errorf("recalleval: aggregate MRR@10 %.3f below floor %.3f", res.MRR10, suite.Floors.MRR10)
	}
	return res, nil
}

// ndcg computes normalized DCG@k against the ideal ranking (all
// expected found = perfect).
func ndcg(ranks []int, totalExpected int) float64 {
	if totalExpected == 0 || len(ranks) == 0 {
		return 0
	}
	dcg := 0.0
	for _, r := range ranks {
		dcg += 1 / math.Log2(float64(r)+1)
	}
	idcg := 0.0
	for i := 1; i <= len(ranks); i++ {
		idcg += 1 / math.Log2(float64(i)+1)
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func mrr(ranks []int) float64 {
	if len(ranks) == 0 {
		return 0
	}
	best := ranks[0]
	for _, r := range ranks {
		if r < best {
			best = r
		}
	}
	return 1 / float64(best)
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func contains(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
