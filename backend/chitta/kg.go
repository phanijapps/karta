package chitta

import (
	"context"
	"strings"
	"time"

	"zbot/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// Knowledge service (knowledge domain)
// ---------------------------------------------------------------------------

// Connect idempotently upserts one typed relationship; endpoints are
// names or ids, missing entities are created by name.
func (s *System) Connect(ctx context.Context, req domain.ConnectRequest, host domain.ChittaHostContext) (domain.Relationship, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.Relationship{}, closedErr
	}
	return emitSeam(s, ctx, ToolConnect, host, func() (domain.Relationship, []string, error) {
		rel, err := s.connectLocked(ctx, drv, req, host)
		return rel, []string{rel.ID}, err
	})
}

func (s *System) connectLocked(ctx context.Context, drv driver, req domain.ConnectRequest, host domain.ChittaHostContext) (domain.Relationship, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" || !validRelType(req.Type) {
		return domain.Relationship{}, domain.NewChittaError(domain.ErrInvalidInput, "from, to, and a snake_case type are required")
	}
	conf := baseConfidence
	if req.Confidence != nil {
		conf = domain.Clamp01(*req.Confidence)
	}
	now := time.Now().UTC()
	from, err := s.resolveOrCreate(ctx, drv, req.From, now)
	if err != nil {
		return domain.Relationship{}, err
	}
	to, err := s.resolveOrCreate(ctx, drv, req.To, now)
	if err != nil {
		return domain.Relationship{}, err
	}
	if existing, found, err := drv.FindRelationship(ctx, from.ID, to.ID, req.Type); err != nil {
		return domain.Relationship{}, domain.NewChittaError(domain.ErrUnavailable, "find relationship: %v", err)
	} else if found {
		existing.Confidence = domain.ConvergeConfidence(existing.Confidence, domain.DefaultConfidenceAlpha)
		if err := drv.UpdateRelationshipConfidence(ctx, existing.ID, existing.Confidence); err != nil {
			return domain.Relationship{}, domain.NewChittaError(domain.ErrUnavailable, "bump relationship: %v", err)
		}
		return existing, nil
	}
	rel := domain.Relationship{
		ID:         newID("rel"),
		From:       from.ID,
		To:         to.ID,
		Type:       req.Type,
		Confidence: conf,
		Provenance: host.Provenance(),
		CreatedAt:  now,
	}
	if err := drv.CreateRelationship(ctx, &rel); err != nil {
		return domain.Relationship{}, domain.NewChittaError(domain.ErrUnavailable, "put relationship: %v", err)
	}
	return rel, nil
}

func (s *System) resolveOrCreate(ctx context.Context, drv driver, ref string, now time.Time) (domain.Entity, error) {
	if e, found, err := s.store.ResolveEntity(ctx, ref); err != nil {
		return domain.Entity{}, domain.NewChittaError(domain.ErrUnavailable, "resolve entity: %v", err)
	} else if found {
		return e, nil
	}
	e, err := s.store.UpsertEntityByName(ctx, ref, domain.EntityOther, nil, nil, now)
	if err != nil {
		return domain.Entity{}, domain.NewChittaError(domain.ErrUnavailable, "create entity: %v", err)
	}
	return e, nil
}

// FindEntities searches names and aliases by prefix and type (rank, name, id).
func (s *System) FindEntities(ctx context.Context, req domain.EntitySearchRequest) (domain.Page[domain.Entity], error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.Page[domain.Entity]{}, closedErr
	}
	if req.Limit <= 0 || req.Limit > domain.RecallMaxLimit {
		req.Limit = domain.RecallDefaultLimit
	}
	return s.store.SearchEntities(ctx, req)
}

// About returns the dossier bridge: knowledge anchor resolved first,
// else subject; active records referencing it; bounded neighborhood.
func (s *System) About(ctx context.Context, req domain.ExploreRequest) (domain.Dossier, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.Dossier{}, closedErr
	}
	if strings.TrimSpace(req.Target) == "" {
		return domain.Dossier{}, domain.NewChittaError(domain.ErrInvalidInput, "target is required")
	}
	depth := req.Depth
	if depth == 0 {
		depth = domain.ExploreDefaultDepth
	}
	if depth < 1 || depth > domain.ExploreMaxDepth {
		return domain.Dossier{}, domain.NewChittaError(domain.ErrInvalidInput, "depth must be 1..%d", domain.ExploreMaxDepth)
	}
	limit := req.Limit
	if limit <= 0 || limit > domain.RecallMaxLimit {
		limit = domain.RecallDefaultLimit
	}
	d := domain.Dossier{Records: domain.Page[domain.MemoryRecord]{Items: []domain.MemoryRecord{}}, Neighborhood: domain.Subgraph{}}

	if e, found, err := drv.ResolveEntity(ctx, req.Target); err != nil {
		return domain.Dossier{}, domain.NewChittaError(domain.ErrUnavailable, "resolve target: %v", err)
	} else if found {
		d.Entity = &e
		page, err := drv.RecordsMentioning(ctx, e.ID, limit, "")
		if err != nil {
			return domain.Dossier{}, domain.NewChittaError(domain.ErrUnavailable, "dossier records: %v", err)
		}
		d.Records = page
		sub, err := drv.Neighborhood(ctx, e.ID, depth, req.Types, limit)
		if err != nil {
			return domain.Dossier{}, domain.NewChittaError(domain.ErrUnavailable, "dossier neighborhood: %v", err)
		}
		d.Neighborhood = sub
		return d, nil
	}
	// Subject fallback: the empty dossier of an unknown subject is a
	// valid, non-error answer.
	subj := strings.TrimSpace(req.Target)
	d.Subject = &subj
	// Dossier record pages order created_at DESC then id (spec
	// "Deterministic results") — queried newest-first, not
	// reversed-after-limit (review round 2: reversal returned the OLDEST
	// subset).
	hist, err := drv.ListRecords(ctx, domain.HistoryRequest{Subject: subj, ActiveOnly: true, NewestFirst: true, Limit: limit})
	if err != nil {
		return domain.Dossier{}, domain.NewChittaError(domain.ErrUnavailable, "subject records: %v", err)
	}
	hist.NextCursor = ""
	d.Records = hist
	return d, nil
}

// UpsertEntity is the trusted host entity write (never agent-facing).
func (s *System) UpsertEntity(ctx context.Context, req domain.EntityUpsert) (domain.Entity, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.Entity{}, closedErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.Name) == "" {
		return domain.Entity{}, domain.NewChittaError(domain.ErrInvalidInput, "name is required")
	}
	if req.Type != "" && !req.Type.Valid() {
		return domain.Entity{}, domain.NewChittaError(domain.ErrInvalidInput, "unknown entity type %q", req.Type)
	}
	return s.store.UpsertEntityByName(ctx, req.Name, req.Type, req.Aliases, req.Confidence, time.Now().UTC())
}

func validRelType(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
