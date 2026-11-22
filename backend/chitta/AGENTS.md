# chitta

Chitta (चित्त, the mind-field) is the core the harness uses to
continuously evolve: one capability holding two first-class domains —
**memory** (experiential, subject-centric records about the user) and
**knowledge** (the entity/relationship graph of the user's world) — in
ONE LadybugDB database at `<data-dir>/chitta/` (default
`~/.zbot/data/chitta/`), disjoint from `zbot.db` (conversations only;
chitta never reads or writes it).

## Responsibilities

- Implements `domain.MemoryStore` + `domain.KnowledgeGraph` (kernel seams
  in `internal/domain/memory.go`).
- Exposes the seven agent tools (`memory_remember`, `memory_recall`,
  `memory_revise`, `memory_history`, `knowledge_connect`,
  `knowledge_find_entities`, `knowledge_about`) through one
  `Dispatch(ctx, tool, args, hostctx)` — transport-neutral (identical
  schemas in-process and over MCP later).
- Evidence-aware writes: contribution-identity idempotency, converging
  confidence `c' = c + (1−c)·α`, `ignored_superseded` (corrections are
  never resurrected), revisions inherit kind, agents never delete.
- One `ChittaToolInvoked` timeline event per tool call (best-effort).
- Conversations never write chitta directly: session content arrives only
  as distilled artifacts from the distillation job through the trusted
  seam (`source: distiller`).

## Boundaries

- Only `internal/ladybug/` imports `github.com/LadybugDB/go-ladybug`
  (cgo, vendored at `third_party/go-ladybug` via go.work + replace).
- Imports only kernel packages downward (`internal/domain`,
  `internal/config`, `internal/eventbus`, `internal/tools`); no kernel
  package imports chitta — the composition root (`cmd/zbotd`) wires it.
- Tool inputs never carry `id`/`provenance`/`status`/timestamps/
  `mention_count`/`last_confirmed_at`/`embedding` — host-assigned, agent
  attempts are `invalid_input`.

## Agent tool plane

`System.ToolProvider(fallback)` exposes the seven tools on the kernel
`tools.Provider` seam (`engine.go`); `chitta.HostContextKey` is how a
caller stamps session/execution/agent identity into the call context —
missing fields fall back to the configured default (source: agent,
root). The composition root merges builtin + chitta via
`tools.Multi`.

## Distillation markers and seam events

`MarkDistilled`/`DistillationMarkers` persist the distiller's
per-session checkpoint in chitta's metadata (`distilled:<sessionID>`,
attempts + dead-letter). Trusted-seam writes (`Remember`/`Revise`/
`Connect`) emit exactly one `ChittaToolInvoked` event ONLY when
`source == distiller`; the agent/user path emits via Dispatch — one
event per logical operation either way. Entity upserts do NOT emit
(deliberate: they are idempotent merge steps, not evidence writes).

## Entrypoints

- `chitta.Open(dir, Options)` — open/create the database, returns the
  `*System` implementing both seams.
- `chitta.Tools()` / `chitta.ToolNames()` — the closed tool inventory.
- `System.Dispatch` — single entry for tool calls.

## Recall pipeline (add-chitta-recall-quality)

Native lanes → RRF (config weights) → utility blend (recency ⊕
confidence ⊕ mentions) → rerank (pluggable; degraded until fastembed-go
ships a reranker) → MMR (λ, over stored embeddings). Keyword lane =
native Okapi BM25 (FTS extension, Porter stemmer) over
`normalized_content`; vector lane = native vector index over the fixed
`FLOAT[E]` embedding column (schema v2; v1 databases migrate at open).
The background producer (fastembed-go, bge-small-en-v1.5) embeds
records and queries; its failure degrades to keyword+graph recall,
never an error. Tuning lives in `chitta.recall.*` config; the golden
harness (`chitta/internal/recalleval`, `task test:chitta:recall`)
gates recall-affecting changes with nDCG@10/MRR@10 floors.

## Engine extensions

FTS + VECTOR extensions load at Open from `third_party/ladybug-ext`
(vendored by `task setup:chitta`; pre-dlopened deterministically — the
daemon never downloads at runtime). ALGO stays for the graph-accuracy
change. Launch note: the vendored dir resolves from the repo/backend
cwd; daemons launched elsewhere must set `ladybug.ExtensionDir` (or run
from `backend/`) — the composition root may wire this from config
later.

## Composition

`cmd/zbotd` opens chitta at startup per the `chitta:` config section
(local provider → LadybugDB at `<data-dir>/chitta/`). Consumers
(distillation job, briefing middleware, embedding producer) attach in
later changes.

## Testing

`go test ./chitta/` runs the suite against an in-memory fake driver;
`CHITTA_DRIVER=ladybug go test ./chitta/` runs the SAME suite plus the
E2E suite (restart persistence, concurrency, data-dir isolation,
dimension mismatch, sparse pagination) against the real LadybugDB
driver. Both must pass.
