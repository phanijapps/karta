# Proposal

## Why

Chitta's recall is only half-built: the keyword lane is a substring
`CONTAINS` scan, the vector lane is dormant (no producer), fusion weights
are guesses, and there is no diversity, reranking, or quality
measurement. For chitta to be the core the harness uses to continuously
evolve, recall must be best-in-class — and LadybugDB already ships the
expensive parts natively (Okapi BM25 with Porter stemming, a vector
index, graph algorithms), which we proved working against our driver.

## What Changes

- **Native keyword lane**: replace substring `CONTAINS` with the engine's
  FTS extension — `QUERY_FTS_INDEX` (Okapi BM25, Porter stemmer, tunable
  K/B, conjunctive mode) over `Memory.normalized_content`, created at
  database init.
- **Native vector lane**: schema v2 migrates `Memory.embedding` from
  `DOUBLE[]` to fixed `FLOAT[E]` (E = configured dimension, 384 default);
  `CREATE_VECTOR_INDEX` at init; `QUERY_VECTOR_INDEX` replaces in-process
  cosine.
- **Embedding producer**: fastembed-go (`BAAI/bge-small-en-v1.5`, 384)
  wired through the trusted seam — batch background embedding of records
  plus host query-embedding injection at recall. BGE query instruction
  prefix applied. Model files fetched at setup, never at runtime.
- **Tuned RRF fusion**: weights per lane become config
  (`chitta.recall.weights`), calibrated by the evaluation harness;
  deterministic total order unchanged.
- **Utility blend**: fused relevance combined with recency decay
  (exponential half-life), confidence prior, and mention reinforcement —
  deterministic, configurable, documented formula.
- **Cross-encoder rerank**: optional stage after fusion —
  `BAAI/bge-reranker-v2-m3` via fastembed-go reranks the top-50 fused
  candidates down to the requested limit (`chitta.recall.rerank`,
  default on; graceful no-op when the model is unavailable).
- **MMR diversification**: maximal marginal relevance (λ configurable,
  default 0.7) over record embeddings post-rerank; skipped gracefully
  when embeddings are absent.
- **Evaluation harness**: golden fixture set + metrics (nDCG@10, MRR@10,
  presence floors), run by `task test:chitta:recall`; recall changes are
  gated by the floors (the agentzero discipline).
- **Vendored extensions**: the FTS/VECTOR/ALGO extension libraries and
  their shared-library dependencies (`libarrow.so.2600`, version-pinned
  `libnetworkit.so` + its `libgomp`) vendored under `third_party/` and
  pre-dlopened by the driver — zero runtime network.
- Wire contracts unchanged: `chitta-contracts` shapes (recall request,
  hits, lanes) are stable; this change is internal quality.

## Capabilities

### New Capabilities
- `chitta-recall`: the retrieval pipeline — native BM25 and vector lanes,
  RRF fusion with calibrated weights, utility blending, cross-encoder
  rerank, MMR diversification, and the evaluation harness with floors.

### Modified Capabilities
- `chitta`: the storage capability's hybrid-recall behavior is upgraded
  to run on the native engine lanes with the quality pipeline behind it.

## Impact

- **Code:** `backend/chitta/internal/ladybug` (native lanes, schema v2
  migration, extension vendoring + pre-dlopen promotion to required);
  new `backend/chitta/recallpipe.go` (fusion/blend/rerank/MMR pipeline);
  new `backend/chitta/embed.go` (fastembed-go producer behind the trusted
  seam); new `backend/chitta/internal/recalleval` (harness + fixtures);
  config gains `chitta.recall.*`.
- **Dependencies:** fastembed-go (+ onnxruntime deps, cgo); vendored
  Ladybug extension libraries under `third_party/ladybug-ext/`.
- **Data:** schema v2 migration (fixed-dimension embedding column,
  vector index build) — automatic at open, guarded by the recorded
  dimension; no export/import.
- **Docs:** chitta package AGENTS.md, config AGENTS.md, Taskfile
  (`test:chitta:recall`).
