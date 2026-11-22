// Package domain: chitta transfer objects and seams.
//
// Chitta is the mind-field — the core the harness uses to continuously
// evolve. It holds two first-class domains: memory (experiential,
// subject-centric records about the user) and knowledge (the entity/
// relationship graph of the user's world), in one LadybugDB database at
// ~/.zbot/data/chitta/. These types are the wire contract (JSON tags are
// normative, see openspec/changes/add-chitta); every participant — the
// tool surface, this seam, storage providers, future MCP adapters —
// exchanges exactly these shapes. The backend/chitta package implements
// the interfaces; nothing here imports storage drivers.

package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Enums (closed sets; wire values are snake_case)
// ---------------------------------------------------------------------------

// MemoryKind is the portable category of a memory record.
type MemoryKind string

const (
	// MemoryKindFact — MemoryKind values — the record kinds chitta stores.
	MemoryKindFact MemoryKind = "fact"
	// MemoryKindPreference — one of the MemoryKind values.
	MemoryKindPreference MemoryKind = "preference"
	// MemoryKindCorrection — one of the MemoryKind values.
	MemoryKindCorrection MemoryKind = "correction"
	// MemoryKindObservation — one of the MemoryKind values.
	MemoryKindObservation MemoryKind = "observation"
	// MemoryKindProcedure — one of the MemoryKind values.
	MemoryKindProcedure MemoryKind = "procedure"
)

// Valid reports whether the kind is a member of the closed enum.
func (k MemoryKind) Valid() bool {
	switch k {
	case MemoryKindFact, MemoryKindPreference, MemoryKindCorrection, MemoryKindObservation, MemoryKindProcedure:
		return true
	}
	return false
}

// MemoryStatus is the record lifecycle. active -> superseded via revise is
// the only transition; nothing deletes.
type MemoryStatus string

const (
	// MemoryStatusActive — MemoryStatus values — a record lifecycle state.
	MemoryStatusActive MemoryStatus = "active"
	// MemoryStatusSuperseded — one of the MemoryStatus values.
	MemoryStatusSuperseded MemoryStatus = "superseded"
)

// Valid reports membership in the closed status set.
func (s MemoryStatus) Valid() bool { return s == MemoryStatusActive || s == MemoryStatusSuperseded }

// EntityType classifies knowledge-graph entities.
type EntityType string

// Valid reports membership in the closed type set.
func (e EntityType) Valid() bool {
	switch e {
	case EntityPerson, EntityProject, EntityTool, EntityOrganization, EntityPlace, EntityConcept, EntityOther:
		return true
	}
	return false
}

const (
	// EntityPerson — EntityType values — knowledge-graph node kinds.
	EntityPerson EntityType = "person"
	// EntityProject — exported library symbol.
	EntityProject EntityType = "project"
	// EntityTool — exported library symbol.
	EntityTool EntityType = "tool"
	// EntityOrganization — exported library symbol.
	EntityOrganization EntityType = "organization"
	// EntityPlace — exported library symbol.
	EntityPlace EntityType = "place"
	// EntityConcept — exported library symbol.
	EntityConcept EntityType = "concept"
	// EntityOther — exported library symbol.
	EntityOther EntityType = "other"
)

// MemorySource names who wrote a record. Agents and users write through
// the tool surface; the distillation backend job writes through the
// trusted seam. Conversations never write chitta directly (design D10).
type MemorySource string

const (
	// SourceAgent — MemorySource values — who wrote the record.
	SourceAgent MemorySource = "agent"
	// SourceUser — one of the MemorySource values.
	SourceUser MemorySource = "user"
	// SourceDistiller — one of the MemorySource values.
	SourceDistiller MemorySource = "distiller"
)

// Valid reports membership in the closed source set.
func (s MemorySource) Valid() bool {
	return s == SourceAgent || s == SourceUser || s == SourceDistiller
}

// RecallLane names a retrieval lane that matched a hit.
type RecallLane string

const (
	// LaneKeyword — RecallLane values — the retrieval lanes a recall may fuse.
	LaneKeyword RecallLane = "keyword"
	// LaneVector — one of the RecallLane values.
	LaneVector RecallLane = "vector"
	// LaneGraph — one of the RecallLane values.
	LaneGraph RecallLane = "graph"
)

// RememberDisposition is the outcome of one remembered observation.
type RememberDisposition string

const (
	// DispositionCreated — RememberDisposition values — what Remember did to the record.
	DispositionCreated RememberDisposition = "created"
	// DispositionMerged — one of the RememberDisposition values.
	DispositionMerged RememberDisposition = "merged"
	// DispositionIgnoredSuperseded — one of the RememberDisposition values.
	DispositionIgnoredSuperseded RememberDisposition = "ignored_superseded"
)

// ChittaErrorCode is the closed error-code set (contracts "Error envelope").
type ChittaErrorCode string

const (
	// ErrInvalidInput — ChittaErrorCode values — typed chitta error codes.
	ErrInvalidInput ChittaErrorCode = "invalid_input"
	// ErrNotFound — one of the ChittaErrorCode values.
	ErrNotFound ChittaErrorCode = "not_found"
	// ErrConflict — one of the ChittaErrorCode values.
	ErrConflict ChittaErrorCode = "conflict"
	// ErrUnavailable — one of the ChittaErrorCode values.
	ErrUnavailable ChittaErrorCode = "unavailable"
)

// ChittaError is the typed error every chitta operation returns.
type ChittaError struct {
	Code    ChittaErrorCode `json:"code"`
	Message string          `json:"message"`
	Detail  string          `json:"detail,omitempty"`
}

func (e *ChittaError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("chitta %s: %s (%s)", e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("chitta %s: %s", e.Code, e.Message)
}

// NewChittaError builds a typed error.
func NewChittaError(code ChittaErrorCode, format string, args ...any) *ChittaError {
	return &ChittaError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------------
// Provenance, host context, evidence
// ---------------------------------------------------------------------------

// MemoryProvenance is host-stamped on every write; callers can never
// supply or alter it through any surface.
type MemoryProvenance struct {
	Source      MemorySource `json:"source"`
	SessionID   string       `json:"session_id,omitempty"`
	ExecutionID string       `json:"execution_id,omitempty"`
	AgentID     string       `json:"agent_id,omitempty"`
	MessageSeq  *int64       `json:"message_seq,omitempty"`
}

// ChittaHostContext carries the trust material the composition root and
// tool dispatch layer stamp into provenance. It is the ONLY source of
// provenance — agents cannot forge it.
type ChittaHostContext struct {
	Source      MemorySource
	SessionID   string
	ExecutionID string
	AgentID     string
	MessageSeq  *int64
	// QueryEmbedding is host-supplied per recall call (never agent input).
	QueryEmbedding []float64
}

// Provenance stamps this context into a provenance object.
func (c ChittaHostContext) Provenance() MemoryProvenance {
	return MemoryProvenance{
		Source:      c.Source,
		SessionID:   c.SessionID,
		ExecutionID: c.ExecutionID,
		AgentID:     c.AgentID,
		MessageSeq:  c.MessageSeq,
	}
}

// MemoryContribution is one durable piece of evidence behind a record:
// the ledger that makes merge idempotency and confidence convergence
// auditable. Replaying an identical contribution changes nothing.
type MemoryContribution struct {
	// ContentHash is both the identity and the key (source, session,
	// execution, message seq, subject, kind, normalized content).
	RecordID    string       `json:"record_id"`
	Source      MemorySource `json:"source"`
	SessionID   string       `json:"session_id,omitempty"`
	ExecutionID string       `json:"execution_id,omitempty"`
	MessageSeq  *int64       `json:"message_seq,omitempty"`
	ContentHash string       `json:"content_hash"`
	CreatedAt   time.Time    `json:"created_at"`
}

// ConvergeConfidence applies the contracts' converging update: distinct
// evidence moves confidence toward 1.0 by alpha, never reaching it by
// repetition alone. c' = c + (1 - c) * alpha.
func ConvergeConfidence(c, alpha float64) float64 {
	if c < 0 {
		c = 0
	}
	if c > 1 {
		c = 1
	}
	next := c + (1-c)*alpha
	if next > 1 {
		next = 1
	}
	return next
}

// DefaultConfidenceAlpha is the contracts' default convergence rate.
const DefaultConfidenceAlpha = 0.3

// Clamp01 clamps a value into [0,1] (agent-suggested confidence).
func Clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// ContributionIdentity hashes the contribution identity fields (source,
// session, execution, message seq, normalized content) into the stable
// idempotency key. Same identity -> same replay is a no-op.
func ContributionIdentity(source MemorySource, sessionID, executionID string, messageSeq *int64, subject string, kind MemoryKind, normalizedContent string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s",
		source, sessionID, executionID, seqString(messageSeq), subject, kind, normalizedContent)))
	return hex.EncodeToString(h[:16])
}

func seqString(seq *int64) string {
	if seq == nil {
		return ""
	}
	return fmt.Sprintf("%d", *seq)
}

// DecodeChittaWrite decodes a write payload strictly: unknown fields are
// invalid_input (contracts "Wire serialization rules" — writes reject,
// reads tolerate). Every write surface uses this.
func DecodeChittaWrite(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return NewChittaError(ErrInvalidInput, "unknown or malformed field: %v", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Memory domain transfer objects
// ---------------------------------------------------------------------------

// MemoryRecord is one experiential, subject-centric record. JSON field
// names are normative (contracts "Memory record object"); optional-absent
// fields are omitted, never null.
type MemoryRecord struct {
	ID              string           `json:"id"`
	Kind            MemoryKind       `json:"kind"`
	Subject         string           `json:"subject"`
	Content         string           `json:"content"`
	Tags            []string         `json:"tags,omitempty"`
	Confidence      float64          `json:"confidence"`
	Status          MemoryStatus     `json:"status"`
	Supersedes      string           `json:"supersedes,omitempty"`
	SupersededBy    string           `json:"superseded_by,omitempty"`
	Entities        []string         `json:"entities,omitempty"`
	Provenance      MemoryProvenance `json:"provenance"`
	MentionCount    int64            `json:"mention_count"`
	LastConfirmedAt *time.Time       `json:"last_confirmed_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// Observation is one item of a memory_remember batch (a write DTO: it
// carries no host-assigned fields).
type Observation struct {
	Kind       MemoryKind `json:"kind"`
	Content    string     `json:"content"`
	Subject    string     `json:"subject,omitempty"` // default "user"
	Tags       []string   `json:"tags,omitempty"`
	Confidence *float64   `json:"confidence,omitempty"`
	Entities   []string   `json:"entities,omitempty"` // names or ids
}

// RememberResult pairs each observation with its stored record and the
// disposition of the write.
type RememberResult struct {
	Disposition RememberDisposition `json:"disposition"`
	Record      *MemoryRecord       `json:"record,omitempty"`
}

// ReviseRequest replaces one active record; the revision inherits the
// superseded record's kind unless Kind is set.
type ReviseRequest struct {
	Supersedes string      `json:"supersedes"`
	Kind       *MemoryKind `json:"kind,omitempty"`
	Content    string      `json:"content"`
	Tags       []string    `json:"tags,omitempty"`
	Confidence *float64    `json:"confidence,omitempty"`
}

// RecallRequest is the memory_recall input. Limit defaults to 10, max 100.
type RecallRequest struct {
	Query             string       `json:"query"`
	Kinds             []MemoryKind `json:"kinds,omitempty"`
	Tags              []string     `json:"tags,omitempty"`
	Subject           string       `json:"subject,omitempty"`
	EntityIDs         []string     `json:"entity_ids,omitempty"`
	Since             *time.Time   `json:"since,omitempty"`
	Until             *time.Time   `json:"until,omitempty"`
	Limit             int          `json:"limit,omitempty"`
	IncludeSuperseded bool         `json:"include_superseded,omitempty"`
}

// Recall limits (contracts "Recall request").
const (
	RecallDefaultLimit = 10
	RecallMaxLimit     = 100
)

// RecallHit is one ranked recall result with lane attribution.
type RecallHit struct {
	Record MemoryRecord `json:"record"`
	Score  float64      `json:"score"`
	Lanes  []RecallLane `json:"lanes"`
}

// HistoryRequest is the memory_history input: the audit view. Superseded
// records are visible by default; ActiveOnly opts out. IDs is the
// direct-id retrieval surface.
type HistoryRequest struct {
	Subject     string       `json:"subject,omitempty"`
	Kinds       []MemoryKind `json:"kinds,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Since       *time.Time   `json:"since,omitempty"`
	Until       *time.Time   `json:"until,omitempty"`
	IDs         []string     `json:"ids,omitempty"`
	ActiveOnly  bool         `json:"active_only,omitempty"`
	NewestFirst bool         `json:"newest_first,omitempty"`
	Cursor      string       `json:"cursor,omitempty"`
	Limit       int          `json:"limit,omitempty"`
}

// VectorHit is one vector-lane match (record id + similarity score).
type VectorHit struct {
	RecordID string  `json:"record_id"`
	Score    float64 `json:"score"`
}

// RecordEmbedding is a trusted host embedding write (never agent input).
type RecordEmbedding struct {
	RecordID  string    `json:"record_id"`
	Embedding []float64 `json:"embedding"`
}

// ---------------------------------------------------------------------------
// Knowledge domain transfer objects
// ---------------------------------------------------------------------------

// Entity is one node of the user's world.
type Entity struct {
	ID              string     `json:"id"`
	Type            EntityType `json:"type"`
	Name            string     `json:"name"`
	NormalizedName  string     `json:"normalized_name"`
	Aliases         []string   `json:"aliases,omitempty"`
	Confidence      float64    `json:"confidence"`
	MentionCount    int64      `json:"mention_count"`
	LastMentionedAt *time.Time `json:"last_mentioned_at,omitempty"`
}

// Relationship is one typed, directed edge.
type Relationship struct {
	ID         string           `json:"id"`
	From       string           `json:"from"` // entity id
	To         string           `json:"to"`   // entity id
	Type       string           `json:"type"` // snake_case verb, open vocabulary
	Confidence float64          `json:"confidence"`
	Provenance MemoryProvenance `json:"provenance"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ConnectRequest is the knowledge_connect input. From/To are names or ids.
type ConnectRequest struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Type       string   `json:"type"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// EntityUpsert is the trusted host entity write (never agent input).
type EntityUpsert struct {
	Name       string     `json:"name"`
	Type       EntityType `json:"type,omitempty"`
	Aliases    []string   `json:"aliases,omitempty"`
	Confidence *float64   `json:"confidence,omitempty"`
}

// EntitySearchRequest is the knowledge_find_entities input.
type EntitySearchRequest struct {
	Prefix string     `json:"prefix,omitempty"`
	Type   EntityType `json:"type,omitempty"`
	Limit  int        `json:"limit,omitempty"`
}

// ExploreRequest is the knowledge_about input. Depth is 1-3, default 1.
type ExploreRequest struct {
	Target string   `json:"target"` // entity name, alias, or id — else subject
	Depth  int      `json:"depth,omitempty"`
	Types  []string `json:"types,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

// Explore depth bounds (contracts "Dossier tool semantics").
const (
	ExploreDefaultDepth = 1
	ExploreMaxDepth     = 3
)

// Subgraph is a set of entities and relationships.
type Subgraph struct {
	Nodes []Entity       `json:"entities,omitempty"`
	Edges []Relationship `json:"relationships,omitempty"`
}

// Dossier is the knowledge_about output: the bridge view joining a
// knowledge anchor to the memory records referencing it and the graph
// neighborhood. Every component is empty-able, never null.
type Dossier struct {
	Entity       *Entity            `json:"entity,omitempty"`
	Subject      *string            `json:"subject,omitempty"`
	Records      Page[MemoryRecord] `json:"records"`
	Neighborhood Subgraph           `json:"neighborhood"`
}

// ---------------------------------------------------------------------------
// Page envelope
// ---------------------------------------------------------------------------

// DistillationMarker is the distiller's checkpoint state for one
// session (trusted host op; persisted in chitta's metadata).
type DistillationMarker struct {
	Attempts   int       `json:"attempts"`
	DeadLetter bool      `json:"dead"`
	At         time.Time `json:"at"`
}

// ObservationOp is one prepared observation write inside an atomic
// remember batch (the driver seam's batch unit).
type ObservationOp struct {
	// Create is set for new records.
	Create *MemoryRecord
	// MergeID is set for merges into an existing record.
	MergeID       string
	NewConfidence float64
	LastConfirmed time.Time
	Updated       time.Time
	AddEntities   []string
	Contribution  *MemoryContribution
}

// RecallSettings carries the pipeline tuning knobs (config-driven;
// calibrated by the evaluation harness).
type RecallSettings struct {
	// Lane weights for RRF (keyword, vector, graph).
	WeightKeyword float64
	WeightVector  float64
	WeightGraph   float64
	// Utility blend: final = fused x (1 + aR.recency + aC.conf + aM.ln(1+mentions)).
	BlendRecency    float64
	BlendConfidence float64
	BlendMentions   float64
	// RecencyHalfLifeDays for exponential decay.
	RecencyHalfLifeDays float64
	// MMR lambda (0=pure relevance, 1=pure diversity); 0 disables.
	MMRLambda float64
	// Rerank enables the cross-encoder stage when a reranker is present.
	Rerank bool
	// RerankTop is the candidate count fed to the reranker.
	RerankTop int
}

// DefaultRecallSettings are the eval-harness-calibrated defaults.
func DefaultRecallSettings() RecallSettings {
	return RecallSettings{
		WeightKeyword: 1.0, WeightVector: 1.0, WeightGraph: 0.8,
		BlendRecency: 0.3, BlendConfidence: 0.15, BlendMentions: 0.1,
		RecencyHalfLifeDays: 30,
		MMRLambda:           0.7,
		Rerank:              true, RerankTop: 50,
	}
}

// Page is the single list envelope (contracts "Page envelope and cursor").
// An absent NextCursor means the end of the result set. Cursors are opaque.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ---------------------------------------------------------------------------
// Seams — implemented by backend/chitta; consumers depend only on these
// ---------------------------------------------------------------------------

// MemoryStore is the memory-domain seam: the tool operations plus the
// trusted host operations (distiller writes, embedding writes, query
// embedding injection). Provenance comes exclusively from the host
// context; agents never supply it.
type MemoryStore interface {
	// Remember writes a bounded, all-or-nothing batch of observations.
	Remember(ctx context.Context, observations []Observation, host ChittaHostContext) ([]RememberResult, error)
	// Recall answers a ranked hybrid query with lane attribution.
	Recall(ctx context.Context, req RecallRequest, host ChittaHostContext) (Page[RecallHit], error)
	// Revise supersedes one active record (the only retirement path).
	Revise(ctx context.Context, req ReviseRequest, host ChittaHostContext) (MemoryRecord, error)
	// History returns the chronological audit view.
	History(ctx context.Context, req HistoryRequest) (Page[MemoryRecord], error)
	// GetRecord reads one record by id (seam-level direct read).
	GetRecord(ctx context.Context, id string) (MemoryRecord, error)
	// Contributions returns the durable evidence ledger for a record.
	Contributions(ctx context.Context, recordID string) ([]MemoryContribution, error)
	// SetRecordEmbeddings is a trusted host embedding write.
	SetRecordEmbeddings(ctx context.Context, embeddings []RecordEmbedding) error
}

// KnowledgeGraph is the knowledge-domain seam.
type KnowledgeGraph interface {
	// Connect idempotently upserts one typed relationship; endpoints are
	// names or ids, missing entities are created by name.
	Connect(ctx context.Context, req ConnectRequest, host ChittaHostContext) (Relationship, error)
	// FindEntities searches names and aliases by prefix and type.
	FindEntities(ctx context.Context, req EntitySearchRequest) (Page[Entity], error)
	// About returns the dossier bridge for one target.
	About(ctx context.Context, req ExploreRequest) (Dossier, error)
	// UpsertEntity is the trusted host entity write (never agent-facing).
	UpsertEntity(ctx context.Context, req EntityUpsert) (Entity, error)
}
