package chitta

import (
	"context"
	"errors"
	"strings"
	"time"
	"zbot/backend/internal/textutil"

	"zbot/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// batchLimit bounds one remember batch.
const batchLimit = 64

// defaultSubject is the contracts' record subject default.
const defaultSubject = "user"

// baseConfidence is the confidence of a first-seen observation when the
// caller does not suggest one.
const baseConfidence = 0.5

// ---------------------------------------------------------------------------
// Memory service (memory domain)
// ---------------------------------------------------------------------------

// Remember writes a bounded, all-or-nothing batch of observations with
// evidence-aware merge semantics. The service lock serializes logical
// read-modify-write sequences (evidence uniqueness, confidence updates,
// revision decisions) — the driver lock only serializes statements.
func (s *System) Remember(ctx context.Context, observations []domain.Observation, host domain.ChittaHostContext) ([]domain.RememberResult, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return nil, closedErr
	}
	return emitSeam(s, ctx, ToolRemember, host, func() ([]domain.RememberResult, []string, error) {
		res, err := s.rememberLocked(ctx, drv, observations, host)
		ids := []string{}
		for _, r := range res {
			if r.Record != nil {
				ids = append(ids, r.Record.ID)
			}
		}
		return res, ids, err
	})
}

// rememberLocked runs the batch against a store snapshot (no re-lock).
func (s *System) rememberLocked(ctx context.Context, drv driver, observations []domain.Observation, host domain.ChittaHostContext) ([]domain.RememberResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(observations) == 0 {
		return nil, domain.NewChittaError(domain.ErrInvalidInput, "empty observation batch")
	}
	if len(observations) > batchLimit {
		return nil, domain.NewChittaError(domain.ErrInvalidInput, "batch of %d exceeds limit %d", len(observations), batchLimit)
	}
	// Validate all first: all-or-nothing.
	items := make([]preparedObservation, 0, len(observations))
	for _, o := range observations {
		if !o.Kind.Valid() {
			return nil, domain.NewChittaError(domain.ErrInvalidInput, "unknown kind %q", o.Kind)
		}
		if strings.TrimSpace(o.Content) == "" {
			return nil, domain.NewChittaError(domain.ErrInvalidInput, "observation content must not be empty")
		}
		conf := baseConfidence
		if o.Confidence != nil {
			conf = domain.Clamp01(*o.Confidence)
		}
		subject := o.Subject
		if subject == "" {
			subject = defaultSubject
		}
		norm := textutil.NormalizeText(o.Content)
		p := preparedObservation{obs: o, subject: subject, conf: conf, norm: norm,
			identity: domain.ContributionIdentity(host.Source, host.SessionID, host.ExecutionID, host.MessageSeq, subject, o.Kind, norm)}
		items = append(items, p)
	}

	ops := make([]domain.ObservationOp, 0, len(items))
	results := make([]domain.RememberResult, 0, len(items))
	now := time.Now().UTC()
	for _, p := range items {
		res, op, err := s.prepareObservation(ctx, drv, p, host, now)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
		if op != nil {
			ops = append(ops, *op)
		}
	}
	if len(ops) > 0 {
		if err := drv.ApplyObservations(ctx, ops); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// preparedObservation is one validated observation awaiting its write.
type preparedObservation struct {
	obs      domain.Observation
	subject  string
	conf     float64
	norm     string
	identity string
}

// prepareObservation decides one observation's disposition and builds
// its write op — no storage writes happen here; the whole batch commits
// atomically via ApplyObservations (review round 3).
func (s *System) prepareObservation(ctx context.Context, drv driver, p preparedObservation, host domain.ChittaHostContext, now time.Time) (domain.RememberResult, *domain.ObservationOp, error) {
	obs, subject, conf, norm, identity := p.obs, p.subject, p.conf, p.norm, p.identity

	if entry, exists, err := drv.GetContribution(ctx, identity); err != nil {
		return domain.RememberResult{}, nil, domain.NewChittaError(domain.ErrUnavailable, "evidence ledger: %v", err)
	} else if exists {
		// Identical replay: disposition follows the LEDGERED record's
		// current status. Storage failures propagate; a missing ledger
		// target is an explicit integrity error (review round 3).
		rec, err := drv.GetRecord(ctx, entry.RecordID)
		if err != nil {
			var ce *domain.ChittaError
			if errors.As(err, &ce) && ce.Code == domain.ErrNotFound {
				return domain.RememberResult{}, nil, domain.NewChittaError(domain.ErrNotFound,
					"contribution references missing record %s (ledger integrity)", entry.RecordID)
			}
			return domain.RememberResult{}, nil, err
		}
		if rec.Status == domain.MemoryStatusActive {
			return domain.RememberResult{Disposition: domain.DispositionMerged, Record: &rec}, nil, nil
		}
		return domain.RememberResult{Disposition: domain.DispositionIgnoredSuperseded}, nil, nil
	}

	match, status, ok, err := drv.FindActiveByContent(ctx, subject, obs.Kind, norm)
	if err != nil {
		return domain.RememberResult{}, nil, domain.NewChittaError(domain.ErrUnavailable, "content match: %v", err)
	}
	if ok && status == domain.MemoryStatusSuperseded {
		// Corrections are never resurrected.
		return domain.RememberResult{Disposition: domain.DispositionIgnoredSuperseded}, nil, nil
	}

	linked, err := s.resolveMentions(ctx, drv, obs.Content, obs.Entities)
	if err != nil {
		return domain.RememberResult{}, nil, err
	}
	contrib := &domain.MemoryContribution{
		Source:      host.Source,
		SessionID:   host.SessionID,
		ExecutionID: host.ExecutionID,
		MessageSeq:  host.MessageSeq,
		ContentHash: identity,
		CreatedAt:   now,
	}

	if ok {
		newConf := domain.ConvergeConfidence(match.Confidence, domain.DefaultConfidenceAlpha)
		match.Confidence = newConf
		match.MentionCount++
		match.LastConfirmedAt = &now
		for _, id := range linked {
			match.Entities = appendUnique(match.Entities, id)
		}
		contrib.RecordID = match.ID
		return domain.RememberResult{Disposition: domain.DispositionMerged, Record: &match},
			&domain.ObservationOp{
				MergeID: match.ID, NewConfidence: newConf,
				LastConfirmed: now, Updated: now,
				AddEntities: linked, Contribution: contrib,
			}, nil
	}

	record := &domain.MemoryRecord{
		ID:              newID("mem"),
		Kind:            obs.Kind,
		Subject:         subject,
		Content:         strings.TrimSpace(obs.Content),
		Tags:            obs.Tags,
		Confidence:      conf,
		Status:          domain.MemoryStatusActive,
		Entities:        linked,
		Provenance:      host.Provenance(),
		MentionCount:    1,
		LastConfirmedAt: &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	contrib.RecordID = record.ID
	return domain.RememberResult{Disposition: domain.DispositionCreated, Record: record},
		&domain.ObservationOp{Create: record, Contribution: contrib}, nil
}

// Revise — Revise applies an explicit edit to an existing memory record.
func (s *System) Revise(ctx context.Context, req domain.ReviseRequest, host domain.ChittaHostContext) (domain.MemoryRecord, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return domain.MemoryRecord{}, closedErr
	}
	return emitSeam(s, ctx, ToolRevise, host, func() (domain.MemoryRecord, []string, error) {
		rec, err := s.reviseLocked(ctx, drv, req, host)
		return rec, []string{rec.ID, req.Supersedes}, err
	})
}

func (s *System) reviseLocked(ctx context.Context, drv driver, req domain.ReviseRequest, host domain.ChittaHostContext) (domain.MemoryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Supersedes == "" || strings.TrimSpace(req.Content) == "" {
		return domain.MemoryRecord{}, domain.NewChittaError(domain.ErrInvalidInput, "supersedes and content are required")
	}
	old, err := drv.GetRecord(ctx, req.Supersedes)
	if err != nil {
		return domain.MemoryRecord{}, err
	}
	if old.Status != domain.MemoryStatusActive {
		return domain.MemoryRecord{}, domain.NewChittaError(domain.ErrConflict, "record %s is already superseded", req.Supersedes)
	}
	now := time.Now().UTC()
	kind := old.Kind // inheritance unless explicit
	if req.Kind != nil {
		kind = *req.Kind
		if !kind.Valid() {
			return domain.MemoryRecord{}, domain.NewChittaError(domain.ErrInvalidInput, "unknown kind %q", kind)
		}
	}
	conf := old.Confidence
	if req.Confidence != nil {
		conf = domain.Clamp01(*req.Confidence)
	}
	revision := domain.MemoryRecord{
		ID:              newID("mem"),
		Kind:            kind,
		Subject:         old.Subject,
		Content:         strings.TrimSpace(req.Content),
		Tags:            req.Tags,
		Confidence:      conf,
		Status:          domain.MemoryStatusActive,
		Supersedes:      old.ID,
		Entities:        old.Entities,
		Provenance:      host.Provenance(),
		MentionCount:    1,
		LastConfirmedAt: &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	revContrib := &domain.MemoryContribution{
		RecordID:    revision.ID,
		Source:      host.Source,
		SessionID:   host.SessionID,
		ExecutionID: host.ExecutionID,
		MessageSeq:  host.MessageSeq,
		ContentHash: domain.ContributionIdentity(host.Source, host.SessionID, host.ExecutionID, host.MessageSeq, old.Subject, kind, textutil.NormalizeText(revision.Content)),
		CreatedAt:   now,
	}
	if err := drv.Supersede(ctx, old.ID, &revision, revContrib); err != nil {
		return domain.MemoryRecord{}, err
	}
	return revision, nil
}

// History returns the chronological audit view (per-tool total order:
// created_at then id).
func (s *System) History(ctx context.Context, req domain.HistoryRequest) (domain.Page[domain.MemoryRecord], error) {
	if req.Limit <= 0 || req.Limit > domain.RecallMaxLimit {
		req.Limit = domain.RecallDefaultLimit
	}
	return s.store.ListRecords(ctx, req)
}

// GetRecord is the seam-level direct read.
func (s *System) GetRecord(ctx context.Context, id string) (domain.MemoryRecord, error) {
	return s.store.GetRecord(ctx, id)
}

// Contributions returns the evidence ledger for one record.
func (s *System) Contributions(ctx context.Context, recordID string) ([]domain.MemoryContribution, error) {
	return s.store.ListContributions(ctx, recordID)
}

// SetRecordEmbeddings is the trusted host embedding write.
func (s *System) SetRecordEmbeddings(ctx context.Context, embeddings []domain.RecordEmbedding) error {
	return s.store.SetEmbeddings(ctx, embeddings)
}

// resolveMentions links the observation to entities referenced
// explicitly (names or ids) AND mentioned in its content (normalized
// token match against existing entities — conservative: no creation).
func (s *System) resolveMentions(ctx context.Context, drv driver, content string, refs []string) ([]string, error) {
	var linked []string
	for _, ref := range refs {
		e, found, err := drv.ResolveEntity(ctx, ref)
		if err != nil {
			return nil, domain.NewChittaError(domain.ErrUnavailable, "resolve entity: %v", err)
		}
		if found {
			linked = appendUnique(linked, e.ID)
		}
	}
	for _, tok := range splitTokens(textutil.NormalizeText(content)) {
		if e, found, err := drv.ResolveEntity(ctx, tok); err != nil {
			return nil, domain.NewChittaError(domain.ErrUnavailable, "resolve mention: %v", err)
		} else if found {
			linked = appendUnique(linked, e.ID)
		}
	}
	return linked, nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
