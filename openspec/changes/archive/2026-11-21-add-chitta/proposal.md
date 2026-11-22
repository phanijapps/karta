# Proposal

## Why

The daemon persists sessions but has no semantic core: agents start
 every session knowing nothing about the user, and nothing the system
learns survives the session. This change defines and implements
**chitta** — the mind-field the future agent stands on, and **the core
the harness uses to continuously evolve**: every session feeds it,
recall and dossiers orient every agent run, revisions correct the model
over time, and the harness gets measurably better at serving the user.
It is one capability holding two first-class domains — **memory**
(experiential, subject-centric records about the user) and **knowledge**
(the entity/relationship graph of the user's world) — both usable by the
agent through a shared tool surface. Contract shapes are informed by the
Engram memory engine's model (kinds, lifecycle, host-stamped provenance,
lane-based recall, cursor paging), trimmed to what an agent needs to
understand a user. Every session feeds it **only via a backend
distillation job** — conversations never write chitta directly; the
job extracts observations/entities/relationships from finished
sessions and persists them through the trusted seam with distiller
provenance (design D10). LadybugDB (embedded property-graph DB, formerly
Kuzu) backs both domains in **their own database**, not more tables in
the internal store. This change lands the core: contracts, tools, service,
storage, provider selection; the continuous-evolution loop (distillation
worker, session-start briefing middleware, embedding producer) consumes
these seams in immediately following changes.

## What Changes

- New spec capability **`chitta-contracts`** (designed first, everything
  consumes it): the transfer objects — memory records (kind, subject,
  content, tags, confidence, revision chain, provenance), knowledge
  entities and relationships, recall requests/results with lane
  attribution, the dossier, page envelopes, typed error codes, and the
  wire serialization rules (JSON, snake_case, RFC3339, closed enums,
  additive-only evolution).
- New spec capability **`chitta-tools`**: the seven agent-facing tools
  across two namespaces — memory: `memory_remember` (bounded batch,
  all-or-nothing, dedupe-merge, auto entity linking), `memory_recall`
  (ranked hybrid retrieval with lane attribution), `memory_revise`
  (revision chains), `memory_history` (chronological audit incl.
  superseded); knowledge: `knowledge_connect` (relationship upsert by
  name or id), `knowledge_find_entities` (prefix/type search),
  `knowledge_about` (dossier bridge: knowledge anchor + memory records +
  bounded neighborhood) — transport-neutral (identical names and schemas
  as in-process engine tools today and as MCP tools when an external
  provider is wired later). Trust rules: provenance is host-stamped
  (agents cannot forge origin), agents revise — never delete. Entity
  creation is implicit (name resolution), not a tool.
- New Go package **`backend/chitta`** (first resident of the plugin zone
  — same module, outside `internal/`): owns both domains, imports kernel
  seams downward; kernel never imports it. `cmd/zbotd` is the
  composition root when the first consumer lands.
- Kernel seams in `backend/internal/domain` (`memory.go`):
  `MemoryStore` / `KnowledgeGraph` interfaces exchanging the contract
  types (coarse, JSON-clean — MCP-compatible by construction).
- Provider selection via configuration (`chitta.provider`, default
  `local`; `mcps:<name>` reserved) — unknown provider fails loudly.
- LadybugDB storage at `~/.zbot/data/chitta/` (cgo via
  `github.com/LadybugDB/go-ladybug`, confined to
  `backend/chitta/internal/ladybug/`). `internal/store` and `zbot.db`
  untouched.
- Structured eventbus emission for every write, recall, and graph
  mutation.
- **BREAKING (docs only):** product memory-stack line changes from
  "SQLite + sqlite-vec + FTS5" to "LadybugDB (graph + FTS + vector)";
  `zbot.db` stays SQLite for sessions/Timeline.
- Out of scope: OKF alignment, the MCP-backed provider, the embedding
  producer (decided: fastembed-go + `BAAI/bge-small-en-v1.5`, 384-dim,
  matching the shipped `embedding_dim` default — see design.md), LLM distillation worker, engine recall middleware, HTTP/SSE
  endpoints, engine relocation into the plugin zone.

## Capabilities

### New Capabilities
- `chitta-contracts`: the transfer objects and wire rules every chitta
  participant (tools, seam, providers, future MCP adapters) exchanges.
- `chitta-tools`: the seven agent-facing tools across the `memory_*` and
  `knowledge_*` namespaces, their trust rules and guarantees.
- `chitta`: durable cross-session storage for both domains, hybrid
  recall, provider selection, and observability behind the contracts.

### Modified Capabilities
- `domain-model`: adds the `MemoryStore` and `KnowledgeGraph` interface
  seams in `internal/domain`, typed by the contract objects, implemented
  by `backend/chitta`.

## Impact

- **Code:** new `backend/chitta/**`; new `backend/internal/domain/memory.go`
  (+ purity tests); new `chitta:` config section.
- **Contract:** `contracts/openapi.yaml` gains the `ChittaToolInvoked`
  `TimelineEventKind` value (closed enum) — contract-first with `task
  codegen` BEFORE the event kind is implemented.
- **Dependencies:** `github.com/LadybugDB/go-ladybug` — first cgo dep;
  tradeoff documented in design.md.
- **Architecture rule:** `backend/AGENTS.md` graph rewritten into kernel
  (`internal/`) and plugin (`backend/<capability>/`) zones; **root
  `AGENTS.md` Global Rule 1 amended**: chitta's memory and knowledge data
  live in `~/.zbot/data/chitta/` (LadybugDB), while sessions/Timeline/
  compactions remain in `~/.zbot/data/zbot.db` — local-first unchanged.
- **Docs:** product-context memory-stack line + the chitta definition
  sentence + the "one SQLite database" vision wording updated;
  backend/AGENTS.md zones; package AGENTS.md; `openspec/config.yaml`
  context wording.
- **Data:** new `~/.zbot/data/chitta/` database; no `zbot.db` migration.
