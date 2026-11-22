# Design

## Context

Chitta ships (archived add-chitta): seams, seven tools, evidence ledger,
revision chains, one LadybugDB database, RRF fusion (k=60, guessed
weights), substring keyword scan, in-process cosine vector search,
dormant vector lane. Probes against the engine proved: FTS extension
loads deterministically with our `preloadGlobal` fix; `QUERY_FTS_INDEX`
is Okapi BM25 with Porter stemming and tunable K/B; VECTOR extension
loads; ALGO loads with vendored `libarrow.so.2600` + a version-pinned
`libnetworkit.so` (wheel 11.3 is one vtable symbol off — the pin must
come from the Ladybug build).

## Goals / Non-Goals

**Goals:** best-in-class recall on native engine primitives; every
quality knob config-driven; measurable floors; zero runtime network;
wire contracts untouched.

**Non-Goals:** HTTP/SSE surfaces; GraphRAG community summaries and
entity resolution (that is the graph-accuracy change); learned-sparse
(SPLADE) and query expansion (HyDE/multi-query) — deferred until floors
exist to measure them; PersonalizedPageRank (graph-accuracy change).

## Decisions

### D1: Pipeline shape — one file, stages as functions

`backend/chitta/recallpipe.go` owns the pipeline:

```
lanes:  BM25 (native)  vector (native)  graph (mention match)
          └──────── RRF (weights: config) ────────┘
                     utility blend (recency ⊕ confidence ⊕ mentions)
                     cross-encoder rerank (top-50 → limit, optional)
                     MMR (λ, on embeddings, graceful skip)
                     → Page[RecallHit]  (contracts unchanged)
```

`System.Recall` becomes a thin caller of the pipeline; the driver's
`SearchRecords`/`VectorSearch` swap to native calls behind the same
seam. No stage knows the tool layer; no stage emits events (Dispatch
does, once, as today).

### D2: Native lanes

- BM25: `CREATE_FTS_INDEX('Memory', 'mem_fts', ['normalized_content'], stemmer := 'porter')`
  at init; queries via
  `CALL QUERY_FTS_INDEX('Memory', 'mem_fts', $q, TOP := $k) RETURN node.id AS id, score`.
  Oversample TOP = limit×4 for fusion headroom. K/B stay at engine
  defaults (1.2/0.75); knobs exposed only if the harness demands.
- Vector: schema v2 — `Memory.embedding FLOAT[E]` (fixed, E from the
  recorded dimension; `DOUBLE[]` column dropped in the v2 migration
  executed once at open, guarded by the schema-version node);
  `CREATE_VECTOR_INDEX` at init;
  `CALL QUERY_VECTOR_INDEX(...) RETURN node.id, distance` mapped to
  similarity rank.

### D3: Producer — fastembed-go behind the seam

`backend/chitta/embed.go`: a lazily started background batch worker that
finds records lacking embeddings (driver query) and writes them via
`SetRecordEmbeddings`; recall injects query embeddings via
`ChittaHostContext.QueryEmbedding` computed with the BGE instruction
prefix. Model directory resolved from config (`chitta.embedding.model`),
files present in `third_party/models/` (setup script fetches; runtime
never fetches). Producer failure degrades to keyword+graph recall —
never a recall error.

### D4: Utility blend — documented, deterministic

`final = fused × (1 + α_r·recency + α_c·confidence + α_m·ln(1+mentions))`
where recency = `2^(−age/halflife)` (halflife default 30d), α defaults
0.3/0.15/0.1 — all under `chitta.recall.*`, documented in this file as
the single source. Deterministic; ties still fall through to the RRF
order (score, recency, id).

### D5: Rerank + MMR — optional stages, no contracts change

fastembed-go `Reranker` (`BAAI/bge-reranker-v2-m3`): rerank top-50 →
limit when `chitta.recall.rerank` (default true) and the model dir
exists; missing model = log-once degraded mode, fused order returned.
MMR (λ default 0.7, config): over the candidate embeddings post-rerank;
no embeddings → skip. Both stages preserve the deterministic-total-order
guarantee for identical inputs and state.

### D6: Harness — fixtures, floors, one task

`backend/chitta/internal/recalleval`: YAML fixtures
(`query`, `expect_ids`, `floor`) + a seeded temp-database runner (both
drivers; native lanes exercised via Ladybug). Metrics: nDCG@10, MRR@10,
per-query presence. `task test:chitta:recall` runs it; floors live with
the fixtures. Calibrated lane weights are whatever the harness says —
the shipped defaults must be the harness's best.

### D7: Vendoring — extensions become required

`third_party/ladybug-ext/`: the three `.lbug_extension` files,
`libarrow.so.2600`, the pinned `libnetworkit.so` + `libgomp` (exact pin
recovered from the Ladybug ALGO build; wheel 11.3 fails one symbol).
`loadNativeExtensions` flips to required-from-vendored-path (no
`INSTALL`, no `~/.lbdb` cache dependency), keeping `preloadGlobal` for
determinism. Setup script (Taskfile `setup:chitta`) fetches artifacts
once; CI/dev offline after that.

## Risks / Trade-offs

- [ALGO vendoring pin may drift with engine releases] → pin extension
  files AND libs to the engine version recorded in the driver; bump
  together (a setup script owns the fetch, not ad-hoc downloads).
- [Rerank adds latency (~50 pairs × cross-encoder)] → stage is optional
  and top-bounded; default-on is acceptable for a personal-scale store;
  harness measures the latency budget.
- [Schema v2 migration at open] → single-step, guarded by the
  schema-version node, dimension-checked; rollback = prior database
  directory (no data written by v1 embeddings exists in practice).
- [Utility blend could fight BM25 relevance] → floors in the harness
  catch it; α values are harness-calibrated, not vibes.

## Migration Plan

Schema v2 runs once at open (embedding column fixed-dimension + vector
index creation + FTS index creation). No data export. The dimension
mismatch rule already shipped stays the guard.

## Open Questions

- Exact Ladybug ALGO ↔ NetworkKit version pin (resolved by inspecting
  the vendored extension's linkage during implementation; if unresolvable
  this week, ALGO vendoring slips to the graph-accuracy change without
  affecting this change's lanes).
- MMR λ and blend α defaults post-calibration (harness output decides;
  recorded in this file when set).
