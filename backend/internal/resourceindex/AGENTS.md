# internal/resourceindex

The semantic resource index for intent analysis (change
`add-intent-analysis`): registry skills + delegatable agents + probed
MCP tools, embedded as chitta records of RESERVED kinds
(`resource_skill` / `resource_agent` / `resource_mcp_tool`,
`domain.IsResourceKind`), recalled by query for the intent prompt's
resource slots.

## Responsibility

- `Indexer.Sync(ctx, snap)` — reconcile to the registry snapshot:
  remember new/changed (content-keyed), supersede changed, tombstone
  removed. Idempotent; unchanged re-sync bumps nothing.
- `Indexer.IndexTools(ctx, server, tools)` — upsert one enabled
  server's probed tool list (probe output is not persisted anywhere
  else; probes feed the index).
- `Indexer.Relevant(ctx, goal, limit)` — kind-filtered recall;
  tombstones post-filtered so retired resources never recommend.

## Boundaries

- Thin `System` seam over chitta's exported surface (Remember/Revise/
  Recall/History) — the real `*chitta.System` in production,
  `chitta.OpenInMemory` in tests. No chitta import in production code.
- Derived data ONLY: the registry snapshot and probe results are the
  source of truth; chitta has no hard delete, so removals are
  tombstone-supersede + recall post-filter (recorded in the change
  design).
- Keyword-lane limitation (fake BM25 is token-exact on hyphens;
  multi-term queries with absent terms drop) is the recorded F-30
  caveat territory: the vector lane carries semantic matching when the
  embedder is warm; the wrapper's listing fallback covers cold caches.

## Key entrypoints

`Sync`, `IndexTools`, `Relevant`, `Hit.Line()` (prompt rendering).
