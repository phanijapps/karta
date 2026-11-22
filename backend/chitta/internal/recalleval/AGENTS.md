# chitta/internal/recalleval

The recall evaluation harness: golden fixtures (queries with expected
records and floors), a seeded-database runner over the real LadybugDB
driver, and nDCG@10 / MRR@10 metrics. Floors gate recall-affecting
changes (spec chitta-recall).

- The embedding backfill FAILS CLOSED (F-20): fetch/embed/save errors
  abort the run — a passing score must mean the vector lane ran.
- KNOWN LIMIT (F-30, pinned by TestGoldenFloorsAreVectorInsensitive):
  the golden fixtures are BM25-dominated — vector quality does not yet
  affect outcomes; recalibration is the follow-up.
- Test doubles inject via `runWithEmbedder(suite, embedder)` (per-test
  injection, no global state — parallel-safe); `backfillEmbeddings`
  takes the seam interface so each stage is deterministically poisonable.
