# Spec Delta

## Purpose

The chitta retrieval pipeline: how a query becomes a ranked, diversified,
utility-weighted page of memory hits — on the engine's native BM25 and
vector indices, with calibrated fusion, optional cross-encoder reranking,
and measurable quality floors.

## ADDED Requirements

### Requirement: Native BM25 keyword lane

The keyword lane SHALL run on the engine's native full-text index (Okapi
BM25, Porter stemming) over memory record content, created at database
initialization and kept in sync with writes. Substring scanning SHALL NOT
be used as the keyword lane.

#### Scenario: BM25 relevance discriminates
- **WHEN** two records share a token but only one matches the query's distinctive terms
- **THEN** the matching record ranks above the other with a BM25 score, and stemming makes inflected forms match ("jumps" ↔ "jumping")

### Requirement: Native vector lane

Record embeddings SHALL be stored at the database's configured fixed
dimension and searched through the engine's native vector index. The
vector lane SHALL engage only when the host supplies a query embedding,
and SHALL be absent (not an error) otherwise.

#### Scenario: Vector search over the index
- **WHEN** records carry embeddings at the configured dimension and the host supplies a query embedding
- **THEN** similarity results come from the native vector index and participate in fusion

### Requirement: Embedding producer behind the trusted seam

A local embedding producer (fastembed-go, default model
`BAAI/bge-small-en-v1.5` at the configured dimension) SHALL embed record
content in a background batch through the trusted seam and supply query
embeddings to recall. Model files SHALL be resolved at setup time; the
daemon SHALL NOT fetch models or make network calls at runtime. The BGE
query instruction prefix SHALL be applied to query embeddings.

#### Scenario: New records gain embeddings asynchronously
- **WHEN** a record is created and the producer runs
- **THEN** the record becomes vector-searchable without any agent-facing surface changing

### Requirement: Calibrated RRF fusion

Lane weights for reciprocal-rank fusion SHALL be configurable
(`chitta.recall.weights`), and the evaluation harness SHALL be the
authority for changing defaults. Fusion remains deterministic: identical
inputs and state produce identical orderings, ties broken by score, then
recency, then id.

#### Scenario: Weights are config, not code
- **WHEN** `chitta.recall.weights` is set
- **THEN** fusion uses those weights without recompilation, and the shipped defaults are the eval-harness-calibrated values

### Requirement: Utility blend

The fused relevance score SHALL be combined with deterministic utility
signals: exponential recency decay (configurable half-life), the record's
confidence, and its mention count. The formula and defaults SHALL be
documented in one place and produce stable orderings.

#### Scenario: Recency breaks relevance ties
- **WHEN** two records fuse to the same relevance and one was confirmed more recently
- **THEN** the more recent record ranks first

### Requirement: Cross-encoder rerank stage

When enabled (`chitta.recall.rerank`, default on) and the rerank model is
available, the pipeline SHALL rerank the top fused candidates (default
top-50) with a local cross-encoder (`BAAI/bge-reranker-v2-m3` via
fastembed-go) down to the requested limit. When the model is unavailable
or disabled, recall SHALL succeed with the fused order and no error.

#### Scenario: Rerank reorders fused candidates
- **WHEN** the cross-encoder scores a fused candidate set
- **THEN** the final page reflects cross-encoder relevance above fusion rank

#### Scenario: Graceful without the model
- **WHEN** rerank is enabled but the model files are absent
- **THEN** recall succeeds with the fused ordering and records a degraded-mode log event

### Requirement: MMR diversification

After reranking, the pipeline SHALL apply maximal marginal relevance
(configurable λ, default 0.7) over record embeddings so near-duplicate
hits do not crowd the page. When embeddings are absent, MMR SHALL be
skipped gracefully.

#### Scenario: Near-duplicates do not crowd the page
- **WHEN** three near-identical records and two distinct ones match a query
- **THEN** the page interleaves distinct results rather than listing only the near-duplicates

### Requirement: Evaluation harness with floors

A recall evaluation harness SHALL run a golden fixture set (queries with
expected records) and report nDCG@10, MRR@10, and presence floors. The
harness SHALL run offline in `task test:chitta:recall` and SHALL fail
when results fall below the configured floors; recall-affecting changes
SHALL update or defend the floors.

#### Scenario: Regression trips the floor
- **WHEN** a change drops a floor metric below its configured minimum
- **THEN** `task test:chitta:recall` fails, naming the query and metric

### Requirement: Vendored engine extensions

The engine extension libraries this change uses (FTS, VECTOR) SHALL be
vendored under `third_party/` and loaded from there (pre-dlopened
deterministically by the driver). ALGO is explicitly deferred to the
graph-accuracy change (it additionally requires vendored libarrow +
a version-pinned libnetworkit). The daemon SHALL NOT download
extensions or models at runtime — a cold model cache keeps the producer
off (keyword+graph recall), never a download.

#### Scenario: Cold start with no network
- **WHEN** the daemon starts with no network connectivity
- **THEN** all recall lanes initialize from vendored artifacts
