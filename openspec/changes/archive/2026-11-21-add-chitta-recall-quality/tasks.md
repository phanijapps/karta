# Tasks

## 0. Round-1 review fixes (ridealong, already applied)

- [x] 0.1 Round-1 review fixes: evidence identity scoped by subject+kind (spec delta chitta-contracts), replay-after-revision ignored, service-level mutation mutex, Ladybug driver fixes (per-iteration pagination predicate, statement close + closed-DB guard, entity resolution by id/name/alias, ranked alias-aware entity search, neighborhood column collisions + edge dedup, plain-CREATE relationships), EntityIDs filter implemented, error propagation (no swallowed lane failures), dead code removed (DataSubdir, cursors on recall/find, DebugQuery, extension loading at open, dataDir, hand-rolled sqrt), zbotd composition wiring. Verified by the full round-1 regression suite on both drivers.

## 0b. Explicit deferral register

Deferred chitta consumers with owners (review round 3): distillation job
(sessions → observations via the trusted seam) and session-start
briefing middleware — both tracked as planned follow-up changes
consuming the chitta seams; embedding producer lands WITH this change
(task 2.1); engine tool wiring rides the first engine-integration
change. Nothing else is silently deferred.

## 1. Native lanes + vendoring

- [x] 1.1 Vendor the engine extensions THIS CHANGE USES (FTS + VECTOR) under `third_party/ladybug-ext/`; `task setup:chitta` (tools/lbug-setup) vendors them once; `loadNativeExtensions` loads vendored + pre-dlopened, no `INSTALL` at runtime. ALGO + libarrow + pinned libnetworkit are DEFERRED to the graph-accuracy change (spec amended accordingly; final-gate review). Note: the extension dir resolves from cwd (repo/backend) or the `ladybug.ExtensionDir` override — daemon launch cwd documented in chitta AGENTS.md.
- [x] 1.2 Schema v2: migrate `Memory.embedding` to fixed `FLOAT[E]` from the recorded dimension, guarded by the schema-version node; create the FTS index (`normalized_content`, porter) and vector index at init. Verify: open→reopen round-trip on a v1-created temp database migrates once and stays stable; dimension-mismatch test still fails loudly.
- [x] 1.3 Swap `SearchRecords` to `QUERY_FTS_INDEX` (BM25, TOP := limit×4 oversample) and `VectorSearch` to `QUERY_VECTOR_INDEX`; delete the in-process cosine path. Verify: shared suite green on BOTH drivers; a stemming case ("jumps" ↔ "jumping") passes (spec scenario "BM25 relevance discriminates").

## 2. Producer + pipeline

- [x] 2.1 `embed.go`: fastembed-go producer (`BAAI/bge-small-en-v1.5`, model dir from config, setup-fetched) — background batch embedding via `SetRecordEmbeddings`, query-embedding injection with the BGE instruction prefix; producer failure degrades to keyword+graph. Verify: records gain embeddings asynchronously and become vector-searchable (spec scenario "New records gain embeddings asynchronously"); producer-down recall still succeeds.
- [x] 2.2 `recallpipe.go`: stage pipeline (RRF with `chitta.recall.weights` → utility blend per D4 → optional rerank → MMR); `System.Recall` becomes a thin caller. Verify: determinism tests stay green; recency tie-break case passes ("Recency breaks relevance ties"); near-duplicate interleaving case passes ("Near-duplicates do not crowd the page"); missing-model rerank degrades gracefully.
- [x] 2.3 Config: `chitta.recall.{weights,halflife_days,blend,rerank,mmr_lambda,rerank_top}` with documented defaults; strict validation. Verify: config table tests cover defaults, overrides, and invalid values.

## 3. Harness + floors

- [x] 3.1 `internal/recalleval`: YAML fixtures (query, expect_ids, floor), seeded temp-database runner over both drivers, nDCG@10/MRR@10/presence metrics; `task test:chitta:recall`. Verify: a deliberate regression trips the floor and names the query (spec scenario "Regression trips the floor"); the shipped lane weights are the harness-calibrated values.

## 4. Constitution and docs

- [x] 4.1 Update `backend/chitta/AGENTS.md` (pipeline stages, producer, vendored extensions), config AGENTS.md note for `chitta.recall.*`, design.md records final calibrated defaults. Verify: docs match the shipped defaults; grep finds no stale in-process-cosine claims.

## 5. Verification

- [x] 5.1 Full: `go test -race` (skillhub excluded as pre-existing WIP), `CHITTA_DRIVER=ladybug` suite, `task test:chitta:recall`, golangci-lint zero, boundary greps (fastembed imports confined to `backend/chitta/`; no runtime network — no `http` client outside setup tooling). Verify: all green in one run.

## 6. Three-round chitta simplicity review

- [x] 6.1 Round 1 (gpt-6.1-sol xhigh + glm-5.3 xhigh): full chitta review — dead code, simplicity, understandability, problem-fit for zbot's memory/knowledge needs. Fix all P0/P1 and cheap P2s; record the rest.
- [x] 6.2 Round 2 (gpt-6.1-terra high + gpt-6.1-sol xhigh): verify Round-1 fixes; hunt residual dead code and complexity. Fix findings.
- [x] 6.3 Round 3 (glm-5.3 xhigh + gpt-6.1-terra high): final simplicity/fit pass — confirm zero dead code, every file explainable, verdict ready. Fix or refute every finding.

## 7. Review and hardening

- [x] 7.1 Run the read-only zbot-reviewer subagent over the change scope; fix every P0/P1; fix P2s or record why not in the change summary.
- [x] 7.2 Re-run `task verify` after review fixes; mark complete only when green.
