# Decision: LadybugDB for the memory + graph layer (future)

Product-owner decision, recorded 2026-11-20: the future memory mode and
knowledge-graph layer will use **LadybugDB** (`github.com/LadybugDB/ladybug`,
MIT, C++ with an official Go binding `github.com/LadybugDB/go-ladybug`)
— the embedded property-graph database formerly known as Kuzu.

Why it fits, verified from the project README/repo:

- Embedded and serverless — local-first, in-process, no service to run.
- One engine for the whole memory promise: property-graph model with
  **Cypher**, **native full-text search**, and **native vector indices**
  (replaces the earlier "sqlite-vec + FTS5 + separate graph" plan with a
  single purpose-built engine).
- Columnar disk storage, CSR adjacency indices, vectorized query
  processing, multi-core parallelism, serializable ACID transactions —
  built for analytical graph workloads (distillation queries, recall).
- Actively developed (pushed today at verification time).

**Not now.** Nothing in the current or in-flight changes depends on it;
SQLite remains the only store. When the memory change lands it will:

- add LadybugDB under `~/.zbot/` (a dedicated data dir, e.g.
  `~/.zbot/data/memory/` — exact placement decided by that change),
- amend the constitution's storage rule in the same change (the
  "SQLite + sqlite-vec + FTS5" wording predates this decision),
- own distillation + recall against durable message ids from the chat
  store — never SQLite rowids.

The `add-simple-chat` design's memory reservations (D8: range-based
distillation, FTS/vector/graph referencing stable message ids) were
written engine-agnostic and hold unchanged under this decision.
