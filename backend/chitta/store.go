// Package chitta is the mind-field — the core the harness uses to
// continuously evolve. One capability, two first-class domains: memory
// (experiential, subject-centric records about the user) and knowledge
// (the entity/relationship graph of the user's world), persisted in ONE
// LadybugDB database at <data-dir>/chitta/ (driver: internal/ladybug).
//
// Boundaries: imports only kernel packages downward (internal/domain,
// internal/config, internal/eventbus); no kernel package imports chitta
// back — the composition root (cmd) wires it. Conversations never write
// chitta directly: session content arrives only as distilled artifacts
// from the distillation job through the trusted seam (design D10).
package chitta

import (
	"context"
	"sync"
	"time"

	"zbot/backend/internal/domain"
)

// driver is the package-private storage seam. Implementations live in
// subpackages (internal/ladybug for production, fakes in tests). All
// methods are storage-shaped: no merge/confidence/revision policy here —
// that logic lives in the service layer so every driver behaves
// identically. Multi-statement mutations are COMPOSITE methods that
// commit atomically (review round 2: no partial writes on failure).
type driver interface {
	Close() error

	// Memory records.
	// ApplyObservations atomically persists a WHOLE remember batch: every
	// create (record + lists + links + contribution) and every merge
	// (bump + links + contribution) commits in one transaction — a
	// failure on item N rolls back items 1..N-1 (review round 3).
	ApplyObservations(ctx context.Context, ops []domain.ObservationOp) error
	// Supersede atomically flips old to superseded and persists the
	// revision record with its contribution.
	Supersede(ctx context.Context, oldID string, newRec *domain.MemoryRecord, contribution *domain.MemoryContribution) error
	GetRecord(ctx context.Context, id string) (domain.MemoryRecord, error)
	// FindActiveByContent matches subject+kind+normalized content against
	// ACTIVE records (merge check) and SUPERSEDED records (resurrection
	// check). Returns the match and its status; ok=false when none.
	FindActiveByContent(ctx context.Context, subject string, kind domain.MemoryKind, normalizedContent string) (rec domain.MemoryRecord, status domain.MemoryStatus, ok bool, err error)
	ListRecords(ctx context.Context, req domain.HistoryRequest) (domain.Page[domain.MemoryRecord], error)
	// SearchRecords is the keyword lane: native Okapi BM25 over
	// normalized_content, filters applied before truncation.
	SearchRecords(ctx context.Context, req domain.RecallRequest) (domain.Page[domain.MemoryRecord], error)
	// VectorSearch is the vector lane: drivers without embedding support
	// return an empty page, not an error.
	VectorSearch(ctx context.Context, embedding []float64, limit int) ([]domain.VectorHit, error)
	// RecordsMentioning pages records referencing one entity (graph lane
	// and dossier records page).
	RecordsMentioning(ctx context.Context, entityID string, limit int, cursor string) (domain.Page[domain.MemoryRecord], error)

	// Evidence ledger.
	GetContribution(ctx context.Context, identity string) (domain.MemoryContribution, bool, error)
	ListContributions(ctx context.Context, recordID string) ([]domain.MemoryContribution, error)

	// Distillation markers (trusted host): checkpoint state for the
	// distiller, keyed by session id — lives with the writer, zbot.db
	// stays conversations-only.
	MarkDistilled(ctx context.Context, sessionID string, attempts int, deadLetter bool) error
	DistillationMarkers(ctx context.Context) (map[string]domain.DistillationMarker, error)

	// Embeddings (trusted host writes only).
	SetEmbeddings(ctx context.Context, embeddings []domain.RecordEmbedding) error
	// RecordsLackingEmbeddings pages records awaiting the producer.
	RecordsLackingEmbeddings(ctx context.Context, limit int) (ids, contents []string, err error)
	// EmbeddingsFor returns stored embeddings for MMR (missing ids omitted).
	EmbeddingsFor(ctx context.Context, ids []string) (map[string][]float64, error)

	// Knowledge graph.
	UpsertEntityByName(ctx context.Context, name string, entityType domain.EntityType, aliases []string, confidence *float64, now time.Time) (domain.Entity, error)
	ResolveEntity(ctx context.Context, ref string) (domain.Entity, bool, error)
	SearchEntities(ctx context.Context, req domain.EntitySearchRequest) (domain.Page[domain.Entity], error)
	FindRelationship(ctx context.Context, from, to, relType string) (domain.Relationship, bool, error)
	CreateRelationship(ctx context.Context, rel *domain.Relationship) error
	UpdateRelationshipConfidence(ctx context.Context, id string, confidence float64) error
	Neighborhood(ctx context.Context, entityID string, depth int, types []string, limit int) (domain.Subgraph, error)
}

// System is the chitta capability: one handle implementing both seams.
// Construct with Open (local LadybugDB provider).
type System struct {
	store driver
	bus   domain.EventBus // optional; events are best-effort
	// mu serializes logical read-modify-write mutations (evidence
	// uniqueness, confidence convergence, revision decisions) across
	// goroutines; the driver lock serializes statements only.
	mu sync.Mutex
	// embedStop stops the background embedding producer (nil when off).
	embedStop chan struct{}
	// recall holds the pipeline tuning (config-driven).
	recall domain.RecallSettings
	// embedder is the optional producer (nil = keyword+graph recall).
	embedder Embedder
	// reranker is the optional cross-encoder stage (nil = degraded).
	reranker Reranker
}

// ConfigureRecall sets the pipeline tuning. Per-knob semantics: a zero
// (or absent) knob takes its calibrated default — partial overrides
// never zero out other lanes (review finding: all-or-nothing defaults
// silently killed lanes).
func (s *System) ConfigureRecall(cfg domain.RecallSettings) {
	d := domain.DefaultRecallSettings()
	if cfg.WeightKeyword == 0 {
		cfg.WeightKeyword = d.WeightKeyword
	}
	if cfg.WeightVector == 0 {
		cfg.WeightVector = d.WeightVector
	}
	if cfg.WeightGraph == 0 {
		cfg.WeightGraph = d.WeightGraph
	}
	if cfg.BlendRecency == 0 {
		cfg.BlendRecency = d.BlendRecency
	}
	if cfg.BlendConfidence == 0 {
		cfg.BlendConfidence = d.BlendConfidence
	}
	if cfg.BlendMentions == 0 {
		cfg.BlendMentions = d.BlendMentions
	}
	if cfg.RecencyHalfLifeDays == 0 {
		cfg.RecencyHalfLifeDays = d.RecencyHalfLifeDays
	}
	if cfg.MMRLambda == 0 {
		cfg.MMRLambda = d.MMRLambda
	}
	if cfg.RerankTop == 0 {
		cfg.RerankTop = d.RerankTop
	}
	s.recall = cfg
}

// SetEmbedder attaches the producer (nil = off).
func (s *System) SetEmbedder(e Embedder) { s.embedder = e }

// RecordsAwaitingEmbedding exposes the producer queue (harness +
// tooling use of the trusted seam).
func (s *System) RecordsAwaitingEmbedding(ctx context.Context, limit int) ([]string, []string, error) {
	return s.store.RecordsLackingEmbeddings(ctx, limit)
}

// SetReranker attaches the cross-encoder stage (nil = degraded mode).
func (s *System) SetReranker(r Reranker) { s.reranker = r }

// closed marks the system unavailable after Close.
var closedErr = domain.NewChittaError(domain.ErrUnavailable, "chitta system is closed")

// Close releases the storage driver and stops the producer; subsequent
// tool calls degrade with a typed unavailable error.
func (s *System) Close() error {
	if s.embedStop != nil {
		close(s.embedStop)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return nil
	}
	drv := s.store
	s.store = nil
	return drv.Close()
}
