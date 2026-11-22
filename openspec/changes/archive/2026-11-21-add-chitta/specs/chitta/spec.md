# Spec Delta

## Purpose

Durable, cross-session memory and knowledge-graph storage for the daemon:
what is remembered, how it is recalled, how it is observable, and where it
persists — as a subsystem with its own local database, independent of the
session database.

## ADDED Requirements

### Requirement: Dedicated chitta database

The chitta capability SHALL persist BOTH domains — memory records and
the knowledge graph — in ONE dedicated local LadybugDB database rooted
at `<data-dir>/chitta/` (default `~/.zbot/data/chitta/`), including the
cross-domain links between them. Chitta data SHALL NOT be written
anywhere outside `<data-dir>/chitta/` — in particular not into the
conversations database (`zbot.db`), which chitta never reads or writes.
The database SHALL record its embedding dimension at creation and fail
subsequent opens with a typed `unavailable` error on dimension
mismatch, never silently re-index.

#### Scenario: Fresh start creates only the chitta database
- **WHEN** the daemon starts for the first time with chitta enabled
- **THEN** the chitta database is initialized under `<data-dir>/chitta/` and the session database file set is unchanged apart from normal session tables

#### Scenario: Memory survives daemon restarts
- **WHEN** a memory record is saved, the daemon restarts, and the same chitta database is reopened
- **THEN** the record is retrievable with its content and metadata intact

### Requirement: Durable memory records

The system SHALL support saving memory records that carry, at minimum: a
kind (e.g. fact, preference, correction), textual content, optional tags, a
confidence value, creation/modification timestamps, and provenance linking
the record to the session and execution it came from. Reading SHALL filter
by kind, tags, session, and time range, and SHALL paginate deterministically.

#### Scenario: Round-trip with provenance
- **WHEN** a memory record is saved with provenance referencing session S and execution E, then read back
- **THEN** the returned record matches the saved content and retains the S/E provenance and timestamps

#### Scenario: Deterministic filtered listing
- **WHEN** records of mixed kinds and tags are saved and a listing requests one kind ordered by creation time
- **THEN** the result contains only records of that kind in a stable, complete order across pages

### Requirement: Hybrid recall

The system SHALL answer recall queries with a single ranked result list that
combines full-text keyword matching over record content and, when a query
embedding is supplied and records carry embeddings, vector-similarity
matching. Recall SHALL degrade to keyword-only matching when embeddings are
unavailable, and the result ordering SHALL be deterministic for identical
inputs.

#### Scenario: Keyword-only recall works without embeddings
- **WHEN** records without embeddings are saved and a recall query with no query embedding is issued
- **THEN** results are ranked by full-text relevance and non-empty for matching content

#### Scenario: Vector recall participates when embeddings exist
- **WHEN** records carry embeddings and the query supplies an embedding
- **THEN** the ranked list reflects vector similarity fused with keyword matches, and does not error when some records lack embeddings

### Requirement: Knowledge graph storage

The system SHALL store typed, named entities with aliases and confidence,
and typed, directed relationships between entities, each carrying provenance
(session/execution) and timestamps. Entity upserts SHALL be idempotent and
merge on normalized names or existing aliases rather than creating
duplicates.

#### Scenario: Upsert merges by normalized name
- **WHEN** the same entity name is upserted twice with different capitalization or an added alias
- **THEN** one entity remains, with the alias registered and mention metadata updated

#### Scenario: Relationships carry provenance
- **WHEN** a relationship is recorded as extracted from session S execution E
- **THEN** reading the relationship returns its type, direction, endpoints, confidence, and S/E provenance

### Requirement: Graph queries

The system SHALL support: neighbor lookup for an entity (with relationship
types and direction), bounded multi-hop traversal from a starting entity,
name and prefix search over entities and aliases, and extraction of a
subgraph around an entity up to a bounded depth. Empty or missing entities
SHALL return empty results, not errors.

#### Scenario: Bounded traversal
- **WHEN** a chain A→B→C exists and a 1-hop traversal from A is requested
- **THEN** B is returned and C is not

#### Scenario: Prefix search over aliases
- **WHEN** an entity has alias "Postgres" and a prefix search for "postg" is issued
- **THEN** the entity is found via its alias

### Requirement: Chitta work is observable

Every tool invocation SHALL publish one `ChittaToolInvoked` structured
event to the eventbus identifying the tool, outcome, error code on
failure, affected record or entity ids, host provenance, and a
timestamp. Trusted host operations that bypass the tool surface (future
distiller or embedding writes via the seam) SHALL publish events with
equivalent fields per their own change's spec — no chitta operation
SHALL silently skip publication. Publication is best-effort per the
eventbus contract; a failed publication SHALL NOT fail the operation.

#### Scenario: Saving a memory publishes an event
- **WHEN** a subscriber is attached to the eventbus and a memory record is saved through `memory_remember`
- **THEN** the subscriber receives a `ChittaToolInvoked` event naming the tool, the record, and its provenance

#### Scenario: Graph mutation publishes an event
- **WHEN** an entity is upserted through the knowledge graph
- **THEN** the subscriber receives a graph-mutation event identifying the entity and operation

### Requirement: Provider selection via configuration

The daemon SHALL select its chitta provider from user configuration,
defaulting to the built-in local provider (embedded database under
`<data-dir>/chitta/`). Configuration load or provider construction with
an unknown or uninitializable provider SHALL fail with a typed,
actionable error naming the provider — never a silent fallback. (Daemon
lifecycle wiring — constructing the provider at daemon startup — lands
with the first consumer change; until then the guarantee is enforced at
configuration load and provider construction.)

#### Scenario: Default is the local provider
- **WHEN** configuration is loaded with no chitta section and the provider is constructed
- **THEN** the local provider is active and the chitta database initializes under `<data-dir>/chitta/`

#### Scenario: Unknown provider fails loudly
- **WHEN** configuration names a provider that is not known to the daemon
- **THEN** provider construction fails with a typed error naming the configured provider and no chitta data is written

### Requirement: Embedding model is advanced configuration

The embedding model and dimension SHALL be configurable only via the
advanced configuration file (`chitta.embedding.model`,
`chitta.embedding.dim`), defaulting to `BAAI/bge-small-en-v1.5` at 384
dimensions. A replacement model with a different dimension MAY be
configured with an explicit dimension; the settings surface SHALL NOT
expose either knob as user-editable until reindex support exists, and a
database opened with a mismatched dimension fails with a typed error.

#### Scenario: Replacement model with explicit dimensions is accepted
- **WHEN** configuration names another model with an explicit `dim`
- **THEN** the configuration loads and the dimension is recorded at database creation

#### Scenario: Swap without explicit dimensions is rejected
- **WHEN** configuration names a non-default model without `dim`
- **THEN** configuration loading fails with an actionable error (reindex is not yet supported)

### Requirement: Local-first operation

All memory and knowledge-graph operations SHALL execute locally with no
network access, SHALL be safe for concurrent use from multiple goroutines
with transactional (all-or-nothing) semantics per logical operation, and
SHALL surface failures as typed errors rather than panics.

#### Scenario: Concurrent writes both persist
- **WHEN** two memory saves run concurrently
- **THEN** both records persist and each published event corresponds to exactly one saved record

#### Scenario: Closed or missing database yields a typed error
- **WHEN** an operation is attempted against a closed or missing memory database
- **THEN** a typed error is returned to the caller and no partial state is written
