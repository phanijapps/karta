package chitta

import (
	"context"
	"strings"
	"zbot/backend/internal/textutil"

	"zbot/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// Hybrid recall — deterministic late fusion with lane attribution (D4)
// ---------------------------------------------------------------------------

// rrfK is the reciprocal-rank-fusion constant.
const rrfK = 60.0

// Recall answers one query through the quality pipeline (see
// recallpipe.go); this file keeps the graph lane and shared helpers.
func (s *System) Recall(ctx context.Context, req domain.RecallRequest, host domain.ChittaHostContext) (domain.Page[domain.RecallHit], error) {
	if req.Limit > domain.RecallMaxLimit {
		return domain.Page[domain.RecallHit]{}, domain.NewChittaError(domain.ErrInvalidInput, "limit %d exceeds maximum %d", req.Limit, domain.RecallMaxLimit)
	}
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.Page[domain.RecallHit]{}, closedErr
	}
	return s.runPipeline(ctx, drv, req, host)
}

func (s *System) graphLane(ctx context.Context, drv driver, query string, limit int) ([]string, error) {
	q := textutil.NormalizeText(query)
	if q == "" {
		return nil, nil
	}
	var ids []string
	for _, tok := range splitTokens(q) {
		e, found, err := drv.ResolveEntity(ctx, tok)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		page, err := drv.RecordsMentioning(ctx, e.ID, limit, "")
		if err != nil {
			return nil, err
		}
		for _, r := range page.Items {
			ids = append(ids, r.ID)
		}
	}
	return dedupe(ids), nil
}

func matchFilters(req domain.RecallRequest, rec domain.MemoryRecord) bool {
	if len(req.Kinds) > 0 {
		ok := false
		for _, k := range req.Kinds {
			if rec.Kind == k {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if req.Subject != "" && rec.Subject != req.Subject {
		return false
	}
	if len(req.EntityIDs) > 0 && !overlaps(rec.Entities, req.EntityIDs) {
		return false
	}
	if len(req.Tags) > 0 {
		set := map[string]bool{}
		for _, t := range rec.Tags {
			set[t] = true
		}
		for _, want := range req.Tags {
			if !set[want] {
				return false
			}
		}
	}
	if req.Since != nil && rec.CreatedAt.Before(*req.Since) {
		return false
	}
	if req.Until != nil && rec.CreatedAt.After(*req.Until) {
		return false
	}
	return true
}

func overlaps(a, b []string) bool {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}

func splitTokens(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if r == ' ' || r == ',' || r == '.' || r == ';' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
