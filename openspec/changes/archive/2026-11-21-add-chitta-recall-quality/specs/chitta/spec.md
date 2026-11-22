# Spec Delta

## MODIFIED Requirements

### Requirement: Hybrid recall

The system SHALL answer recall queries with a single ranked result list
produced by the chitta-recall pipeline: the native full-text lane always
available, the native vector lane engaging when the host supplies a query
embedding and records carry embeddings, and the graph lane matching
entities referenced by the query. The result ordering SHALL be
deterministic for identical inputs, superseded records excluded unless
requested, and every hit SHALL attribute its matched lanes.

#### Scenario: Keyword-only recall works without embeddings
- **WHEN** records without embeddings are saved and a recall query with no query embedding is issued
- **THEN** results are ranked by the native BM25 lane (with or without the graph lane) and do not error

#### Scenario: Vector recall participates when embeddings exist
- **WHEN** records carry embeddings and the query supplies an embedding
- **THEN** the ranked list reflects vector similarity fused with keyword matches, and does not error when some records lack embeddings

### Requirement: Embedding model is advanced configuration

The embedding model and dimension SHALL be configurable only via the
advanced configuration file (`chitta.embedding.model`,
`chitta.embedding.dim`), defaulting to `BAAI/bge-small-en-v1.5` at 384
dimensions. A replacement model with a different dimension MAY be
configured with an explicit dimension; the settings surface SHALL NOT
expose either knob as user-editable until reindex support exists, and a
database opened with a mismatched dimension fails with a typed error.
The database SHALL store embeddings at the configured fixed dimension
and maintain a native vector index over them.

#### Scenario: Replacement model with explicit dimensions is accepted
- **WHEN** configuration names another model with an explicit `dim`
- **THEN** the configuration loads, the dimension is recorded at database creation, and new records embed at that dimension

#### Scenario: Swap without explicit dimensions is rejected
- **WHEN** configuration names a non-default model without `dim`
- **THEN** configuration loading fails with an actionable error (reindex is not yet supported)
