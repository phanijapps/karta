package chitta

import (
	"context"
	"math"
	"sort"
	"time"

	"zbot/backend/internal/domain"
)

// pipeline stages (design D1 of add-chitta-recall-quality):
//
//	lanes: BM25 (native) | vector (native) | graph (mention match)
//	          └── RRF (weights: RecallSettings) ──┘
//	          utility blend (recency ⊕ confidence ⊕ mentions)
//	          cross-encoder rerank (top-N → limit; pluggable, off until a
//	            Go reranker ships — fastembed-go has none yet)
//	          MMR diversification (λ; over stored embeddings, graceful skip)
//	          → Page[RecallHit]  (contracts unchanged)

// Reranker is the pluggable cross-encoder stage. fastembed-go does not
// ship a reranker yet; when it does, an implementation plugs in here and
// config flips it live. Until then the stage degrades to the fused
// order (spec scenario "Graceful without the model").
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []domain.MemoryRecord, top int) ([]domain.MemoryRecord, error)
}

// runPipeline executes the recall pipeline over the three lanes.
func (s *System) runPipeline(ctx context.Context, drv driver, req domain.RecallRequest, host domain.ChittaHostContext) (domain.Page[domain.RecallHit], error) {
	cfg := s.recall
	limit := req.Limit
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	if limit > domain.RecallMaxLimit {
		return domain.Page[domain.RecallHit]{}, domain.NewChittaError(domain.ErrInvalidInput,
			"limit %d exceeds maximum %d", req.Limit, domain.RecallMaxLimit)
	}

	type accum struct {
		ranks map[domain.RecallLane]int
	}
	byRecord := map[string]*accum{}
	addLane := func(ids []string, lane domain.RecallLane) {
		for i, id := range ids {
			e, ok := byRecord[id]
			if !ok {
				e = &accum{ranks: map[domain.RecallLane]int{}}
				byRecord[id] = e
			}
			if prev, seen := e.ranks[lane]; !seen || i+1 < prev {
				e.ranks[lane] = i + 1
			}
		}
	}

	// Lane 1: native BM25 (always available).
	page, err := drv.SearchRecords(ctx, req)
	if err != nil {
		return domain.Page[domain.RecallHit]{}, err
	}
	ids := make([]string, 0, len(page.Items))
	for _, r := range page.Items {
		ids = append(ids, r.ID)
	}
	addLane(ids, domain.LaneKeyword)

	// Lane 2: native vector (query embedding from the host or producer).
	// Producer failure degrades to skipping the lane (spec
	// "Graceful without the model") — never a recall error.
	queryEmb := host.QueryEmbedding
	if len(queryEmb) == 0 && s.embedder != nil {
		if qe, err := s.embedder.EmbedQuery(ctx, req.Query); err == nil {
			queryEmb = qe
		}
	}
	if len(queryEmb) > 0 {
		hits, err := drv.VectorSearch(ctx, queryEmb, domain.RecallMaxLimit)
		if err != nil {
			return domain.Page[domain.RecallHit]{}, err
		}
		vids := make([]string, 0, len(hits))
		for _, h := range hits {
			vids = append(vids, h.RecordID)
		}
		addLane(vids, domain.LaneVector)
	}

	// Lane 3: graph (entities mentioned in the query).
	graphIDs, err := s.graphLane(ctx, drv, req.Query, domain.RecallMaxLimit)
	if err != nil {
		return domain.Page[domain.RecallHit]{}, err
	}
	addLane(graphIDs, domain.LaneGraph)

	if len(byRecord) == 0 {
		return domain.Page[domain.RecallHit]{Items: []domain.RecallHit{}}, nil
	}

	// Fetch full records, filter, fuse (fixed lane order).
	weight := map[domain.RecallLane]float64{
		domain.LaneKeyword: cfg.WeightKeyword,
		domain.LaneVector:  cfg.WeightVector,
		domain.LaneGraph:   cfg.WeightGraph,
	}
	hits := make([]domain.RecallHit, 0, len(byRecord))
	for id, lr := range byRecord {
		rec, err := drv.GetRecord(ctx, id)
		if err != nil {
			return domain.Page[domain.RecallHit]{}, err
		}
		if !req.IncludeSuperseded && rec.Status != domain.MemoryStatusActive {
			continue
		}
		if !matchFilters(req, rec) {
			continue
		}
		score := 0.0
		lanes := make([]domain.RecallLane, 0, len(lr.ranks))
		for _, lane := range []domain.RecallLane{domain.LaneKeyword, domain.LaneVector, domain.LaneGraph} {
			rank, ok := lr.ranks[lane]
			if !ok {
				continue
			}
			lanes = append(lanes, lane)
			score += weight[lane] / (rrfK + float64(rank))
		}
		hits = append(hits, domain.RecallHit{Record: rec, Score: score, Lanes: lanes})
	}

	now := time.Now().UTC()
	for i := range hits {
		hits[i].Score = blendUtility(hits[i].Score, &hits[i].Record, cfg, now)
	}
	sortHits(hits)

	// Rerank stage (pluggable; degraded without a reranker).
	if cfg.Rerank && s.reranker != nil && len(hits) > limit {
		top := cfg.RerankTop
		if top < limit {
			top = limit
		}
		cands := make([]domain.MemoryRecord, 0, min(len(hits), top))
		for _, h := range hits {
			if len(cands) >= top {
				break
			}
			cands = append(cands, h.Record)
		}
		if reordered, err := s.reranker.Rerank(ctx, req.Query, cands, limit); err == nil && len(reordered) > 0 {
			order := map[string]int{}
			for i, r := range reordered {
				order[r.ID] = i
			}
			byID := map[string]domain.RecallHit{}
			for _, h := range hits {
				byID[h.Record.ID] = h
			}
			next := make([]domain.RecallHit, 0, len(reordered))
			for _, r := range reordered {
				if h, ok := byID[r.ID]; ok {
					next = append(next, h)
				}
			}
			for _, h := range hits { // keep any the reranker dropped
				if _, ok := order[h.Record.ID]; !ok && len(next) < len(hits) {
					next = append(next, h)
				}
			}
			hits = next
		}
	}

	// MMR diversification over stored embeddings (graceful without).
	if cfg.MMRLambda > 0 && len(hits) > limit {
		hits = s.mmr(ctx, drv, req.Query, hits, limit, cfg.MMRLambda)
	}

	if len(hits) > limit {
		hits = hits[:limit]
	}
	for i := range hits {
		l := hits[i].Lanes
		sort.Slice(l, func(a, b int) bool { return l[a] < l[b] })
	}
	return domain.Page[domain.RecallHit]{Items: hits}, nil
}

// blendUtility applies the documented utility blend:
// final = fused x (1 + aR.recency + aC.confidence + aM.ln(1+mentions)).
func blendUtility(fused float64, rec *domain.MemoryRecord, cfg domain.RecallSettings, now time.Time) float64 {
	age := now.Sub(rec.CreatedAt)
	halfLife := time.Duration(cfg.RecencyHalfLifeDays*24) * time.Hour
	if halfLife <= 0 {
		halfLife = time.Hour // guard
	}
	recency := math.Exp2(-age.Hours() / halfLife.Hours())
	return fused * (1 +
		cfg.BlendRecency*recency +
		cfg.BlendConfidence*rec.Confidence +
		cfg.BlendMentions*math.Log1p(float64(rec.MentionCount)))
}

func sortHits(hits []domain.RecallHit) {
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if !a.Record.CreatedAt.Equal(b.Record.CreatedAt) {
			return a.Record.CreatedAt.After(b.Record.CreatedAt)
		}
		return a.Record.ID < b.Record.ID
	})
}

// mmr applies maximal marginal relevance over stored embeddings;
// records without embeddings keep fused order after the embedded ones.
func (s *System) mmr(ctx context.Context, drv driver, query string, hits []domain.RecallHit, limit int, lambda float64) []domain.RecallHit {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.Record.ID)
	}
	embs, err := drv.EmbeddingsFor(ctx, ids)
	if err != nil || len(embs) == 0 {
		return hits // graceful skip
	}
	var queryEmb []float64
	if s.embedder != nil {
		if qe, err := s.embedder.EmbedQuery(ctx, query); err == nil {
			queryEmb = qe
		}
	}
	selected := make([]domain.RecallHit, 0, limit)
	pool := append([]domain.RecallHit(nil), hits...)
	for len(selected) < limit && len(pool) > 0 {
		bestIdx := 0
		bestVal := math.Inf(-1)
		for i, cand := range pool {
			emb, ok := embs[cand.Record.ID]
			if !ok {
				continue // no embedding: cannot score; kept in fused order after
			}
			relevance := cand.Score
			if len(queryEmb) > 0 && len(emb) == len(queryEmb) {
				relevance = cosineOrFused(cand.Score, emb, queryEmb)
			}
			maxSim := 0.0
			for _, sel := range selected {
				if se, ok := embs[sel.Record.ID]; ok && len(se) == len(emb) {
					if sim := dotNorm(se, emb); sim > maxSim {
						maxSim = sim
					}
				}
			}
			val := lambda*relevance - (1-lambda)*maxSim
			if val > bestVal {
				bestVal = val
				bestIdx = i
			}
		}
		selected = append(selected, pool[bestIdx])
		pool = append(pool[:bestIdx], pool[bestIdx+1:]...)
	}
	if len(selected) < len(hits) {
		selected = append(selected, pool...) // keep the rest in fused order
	}
	return selected
}

func cosineOrFused(fallback float64, a, b []float64) float64 {
	if dot := dotNorm(a, b); dot != 0 {
		return dot
	}
	return fallback
}

// dotNorm returns the normalized dot product (cosine) of equal-length
// vectors, or 0 for empty/mismatched inputs.
func dotNorm(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
