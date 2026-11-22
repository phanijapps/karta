# Design

## Context

Today the daemon has exactly one persistence surface: `internal/store`
(modernc.org/sqlite, pure Go) holding the append-only session model in
`~/.zbot/data/zbot.db`. Nothing exists for memory or the knowledge graph,
and agents have no way to build or query a model of the user.

Target architecture (this change makes it literal in the tree): one Go
module; `backend/internal/` = kernel (domain seams, config, registry,
eventbus, store, server, mcpclient); capabilities live outside internal as
pluggable units — `backend/<capability>/` — importing kernel seams
downward, selected via configuration, replaceable by MCP-server
implementations of the same seam. Chitta is the first resident of that
plugin zone.

Contract model is informed by the Engram memory engine (reference only):
its `MemoryKind` taxonomy, host-stamped `Provenance`, lifecycle statuses,
typed links, lane-based recall, and `Page<T>`/cursor paging — trimmed to a
user-model core (see D2). LadybugDB (embedded property-graph DB, ex-Kuzu)
provides Cypher, native FTS, and a native vector index in one in-process
engine (`github.com/LadybugDB/go-ladybug`).

## Goals / Non-Goals

**Goals:**
- Contracts first: transfer objects and tool surface specced and
  implemented before any storage engine, so tools, seam, and a future MCP
  provider all bind to one wire shape.
- `backend/chitta` as a self-contained pluggable package owning memory
  records and the knowledge graph, driver-swappable
  (`backend/chitta/internal/ladybug/`).
- Memory data in its own LadybugDB database at `<data-dir>/chitta/`,
  disjoint from `zbot.db`.
- The semantic layer for understanding the user: subject-centric records,
  first-class corrections, entity graph of the user's world.
- Testable with fake drivers and temp-dir databases; zero network.

**Non-Goals:**
- OKF (Open Knowledge Format) alignment — out of scope for this effort.
- The MCP-backed memory provider (`mcps:<name>` reserved only).
- ONNX embedding producer, LLM distillation worker, engine recall
  middleware, HTTP/SSE endpoints, engine relocation, `zbot.db` migration.
- Engram's research surface: verdicts, assertions, beliefs, contradiction
  detection, hierarchies, forget/redact lifecycles. Corrections supersede;
  nothing else mutates status.

## Decisions

### D1: Package shape — plugin zone resident, contracts in the kernel

```
backend/internal/domain/
  memory.go              # transfer objects + MemoryStore/KnowledgeGraph seams
                         # (wire contract: JSON tags are normative)
backend/chitta/          # plugin package (outside internal/, same module)
  AGENTS.md
  tools.go               # tool definitions: names, input schemas (JSON Schema),
                         # dispatch to seam; host-stamps provenance, mints ids
  memory.go              # record service logic over the driver interface
  kg.go                  # entity/relationship service logic (normalize, merge)
  recall.go              # hybrid recall + fusion (D4)
  store.go               # package-private driver interface
  events.go              # TimelineEvent construction per tool call
  internal/ladybug/      # the only subtree importing go-ladybug
    db.go, memory_store.go, kg_store.go, schema.cypher
  *_test.go              # fake-driver + real-driver suites
```

Transfer objects live in the kernel (`internal/domain`) because every
participant exchanges them; `backend/chitta` imports them downward. No
kernel package imports `backend/chitta`; `cmd/zbotd` is the composition
root. The driver subtree is compiler-protected by the Go internal rule.
Alternative rejected: contracts inside `backend/chitta` — then the seam in
`domain` couldn't be typed by them without the kernel importing the
plugin; alternative rejected: separate go.mod per plugin — cannot import
kernel seams (module-internal rule) without extracting them.

### D2: Contract model — Engram trimmed to a user-model core

From Engram we keep, in trimmed form: portable `kind` taxonomy (their 7 →
our 5: fact, preference, correction, observation, procedure — dropping
`episode`, session-scoped and owned by the session store; `artifact`,
owned by file storage; `relationship-as-kind`, replaced by first-class KG
relationships), host-stamped `Provenance` (their `Provenance` +
`asserted_by_agent/grounded_in_source` collapses to `source:
agent|user|distiller`), lane-based recall (their `lanes:["memory",
"knowledge"]` → per-hit `lanes:[keyword, vector, graph]`), and
`Page<T>`/opaque cursor. We simplify lifecycle: their five statuses →
`active | superseded` only; revisions are the sole retirement path (a
zbot-specific stance: agents never delete). We add `subject` (default
`user`) — Engram scopes workspace/environment/subject at the store level;
for a user-model the subject is a record field, simpler and
agent-addressable. Prefixed ids (`mem_`, `ent_`, `rel_` + sortable ULID
tail) — Engram uses opaque typed ids; prefixes make agent-facing JSON
debuggable. Verdicts/assertions/beliefs: dropped (research layer, not
needed to understand the user).

### D2b: Tool surface — two domains, cognitive verbs, smart writes, implicit entities

The layer holds two first-class domains that are NOT the same thing
(mirroring the Engram reference split: memory is experiential, knowledge
is grounded structure): **memory** — subject-centric records about the
user (facts, preferences, corrections, observations, procedures) with
revision chains; **knowledge** — the graph of the user's world (typed
entities, aliases, relationships). Tools are questions an agent asks or
judgments it makes, not storage verbs, and each tool belongs to exactly
one domain namespace: `memory_remember` (bounded batch, all-or-nothing
validation; exact normalized duplicate → `merged` under the evidence
and confirmation semantics — contribution-identity idempotency,
converging confidence `c' = c + (1−c)·α`, `mention_count` and
`last_confirmed_at` advance; exact match against SUPERSEDED content →
`ignored_superseded`, so corrections are never resurrected),
`memory_recall` (relevance view, superseded hidden by
default), `memory_revise` (sole retirement path; the revision inherits
the superseded record's kind unless an explicit kind is supplied, so a
corrected preference remains filterable as a preference; revising a
superseded record is `conflict`), `memory_history` (audit view,
superseded visible by default, `ids` filter for direct retrieval); `knowledge_connect` (idempotent typed-relationship upsert,
endpoints resolved or created by name), `knowledge_find_entities`
(prefix/type discovery over names and aliases — named so it cannot be
misread as record search), `knowledge_about` (the dossier bridge:
knowledge anchor resolved first, then the memory records referencing it,
plus the bounded neighborhood — orientation is the common session-start
act, and it deliberately joins both domains). Entity mentions in memory
observations resolve and link by normalized name, so no explicit
entity-upsert tool exists (the host and distiller create entities
through the seam when needed). Batch merge is deliberately conservative
— exact normalized match only — until embeddings exist for near-duplicate
merging (follow-up change); this keeps `memory_remember` deterministic
and reviewable.

### D2c: Naming — council-debated, owner-decided: `chitta`

A two-pass model debate (glm-5.3 vs gpt-6.1-sol, independent positions +
cross-exam) settled the mechanical rules: underscore tool namespaces
(dots are an MCP client variance risk; underscore matches official MCP
server convention), seven tools across two domain namespaces, plain
namespaces, plain
identifiers over jargon. Both models recommended plain `memory`
identifiers with an evocative word reserved for prose — and both agreed
the umbrella word is an owner preference, cheapest to settle
pre-implementation. **Owner decision (final, settled): the capability is
`chitta`** — Sanskrit चित्त, the mind-field: the field of consciousness
that holds memory traces, knowledge, and cognition. It names exactly
what this layer is: the core the harness uses to continuously evolve,
holding BOTH domains — memory (experiential records about the user) and
knowledge (the graph of the user's world) — without reducing either to
the other. Identifiers: package `backend/chitta`, config `chitta:`
(`chitta.provider`), specs `chitta-contracts`/`chitta-tools`/`chitta`,
data `~/.zbot/data/chitta/`. Agent-facing tool namespaces stay
`memory_*` and `knowledge_*` — the domains remain visible and separately
usable; chitta is the field, not a tool prefix. Domain seams stay
`domain.MemoryStore` / `domain.KnowledgeGraph` (they describe the
domains; chitta implements both). Timeline events use one uniform
`ChittaToolInvoked` kind (D8) so the SSE contract enum stays minimal
while the payload distinguishes the domain operation.

### D3: Tool binding — one dispatch, many transports

`tools.go` defines each tool as `{name, input JSON Schema derived from the
transfer objects, handler func}` behind a package-level registry with a
single `Dispatch(ctx, tool, args, hostctx)` entry. The host context
carries provenance material (session/execution/agent/message seq) and is
the only source of it — agent-supplied reserved fields are rejected at
schema-dispatch (additionalProperties: false + reserved-field blocklist).
Engine tool binding (this change delivers the registry; engine wiring is
the recall-middleware follow-up) and the future MCP provider both call the
same `Dispatch`, guaranteeing transport neutrality. Coarse single-shot
request/response only — no streaming or callbacks — keeps MCP parity
structural, not aspirational.

### D4: Hybrid recall — deterministic late fusion with lane attribution

`Recall` runs keyword (FTS over `Memory.content`) and vector (when host
query embedding + record embeddings exist) lanes, plus a graph lane
(records referencing entities matched by the query's entity mentions),
fusing with reciprocal-rank fusion (RRF, k=60), ties broken by `(score
desc, created_at desc, id asc)`. Every hit reports the lanes that matched.
RRF needs no cross-lane score-scale agreement — the safe default before
tuning data exists.

### D5: LadybugDB schema

Node tables: `Memory(id, kind, content, tags, confidence, subject,
status, supersedes, superseded_by, mention_count, last_confirmed_at,
embedding FLOAT[E] NULL)`; `Contribution(id, record_id, source,
session_id, execution_id, message_seq, content_hash, created_at)` — the
durable evidence ledger behind merge idempotency and confidence
convergence; `Entity(id, type, name, normalized_name, confidence,
mention_count, last_mentioned_at, embedding FLOAT[E] NULL)`; `Alias(alias,
entity_id)` keyed `(alias, entity_id)`, normalized on write. Record
provenance lives on its contributions; the record's own provenance
object is the origin contribution. Rel tables:
`Relates(from Entity, to Entity, type, confidence, provenance,
created_at)`; `Mentions(memory Memory, entity Entity, created_at)`. The
embedding dimension `E` is fixed at creation, recorded in the
schema-version node, and a mismatched open fails with a typed
`unavailable` error (rebuild-on-dimension-change belongs to the
embeddings follow-up, never a silent re-index). Native FTS over
`Memory.content` and `Entity.name`+aliases; native vector index on
embeddings. Alternative rejected: SQLite + hand-rolled graph —
re-imports the traversal problem Ladybug solves and stuffs memory into the
internal store.

### D6: cgo acceptance

`go-ladybug` is a cgo binding over precompiled C++ libraries — the
daemon's first cgo dependency. Accepted: the desktop target already
requires cgo (Wails v3); in-process embedding keeps local-first ACID
semantics; Ladybug ships prebuilt libs. Contained: the import lives only
in `backend/chitta/internal/ladybug/`; a future driver swap touches that
subpackage alone. Alternative rejected: sidecar process — lifecycle
management, packaging, IPC latency.

### D7: Provider selection — config-driven

`memory:` → `chitta:` config section: `provider` (default `local`; `mcps:<name>`
grammar reserved), embedding dimension, data subdir. Unknown values fail
config load with a typed error naming the provider — loud, never silent —
including reserved-but-unimplemented `mcps:<name>` until that provider
exists.

### D8: Events — one kind, contract-first

One `TimelineEvent` kind — `ChittaToolInvoked` — published for every
tool invocation (success and failure) with payload `{tool, outcome,
error_code?, affected_ids, provenance}`. One uniform kind keeps the
SSE contract enum delta minimal and covers all seven tools including
reads, which the earlier three-kind draft missed. `TimelineEventKind`
is a closed enum in `contracts/openapi.yaml`: the contract and codegen
land BEFORE the event kind is implemented (constitution rule 2;
council P0). Best-effort per the bus contract; bus optional at
construction so storage tests run bare. Future seam-level host writes
(distiller, embeddings) define their event coverage in their own
changes.

### D9: Normalization, idempotency, ids

Entity names/aliases normalize (NFC, casefold, trim, collapse
whitespace); upserts merge on exact normalized match; relationship upserts
idempotent on `(from, to, type)`, bumping confidence/mention metadata.
IDs are ULIDs (sortable, prefix + 48-bit time) minted by the service
layer, never the driver, never the agent.

### D10: Evolution loop — distilled-only, never direct

Conversations live in `zbot.db` (append-only, kernel store). They
NEVER write chitta directly: the distillation job — a backend worker
registered in the composition root (`cmd`), delivered by a follow-up
change — reads finished sessions through the store API, extracts
observations/entities/relationships, and writes through the trusted
chitta seam with `source: distiller` provenance. The job composes both
sides (store for reads, chitta seam for writes); the chitta package
never imports the store and the store never imports chitta — the loop
closes in the composition root, keeping the plugin/kernel boundary
intact. Agent/user tool writes remain the only other write path. Raw
transcripts stay out of chitta; provenance (session/execution/message
seq) points back into the session store for audit.

## Risks / Trade-offs

- [Contract churn while distillation/embeddings land later] → additive-only
  wire rules (spec) bound the blast radius; JSON tags are the single
  normative mapping, generated schemas derived from the same types.
- [Plugin-zone boundary is convention + CI grep, not compiler-enforced for
  kernel→plugin imports] → boundary grep asserts no `backend/internal/`
  package imports `backend/chitta`; driver isolation IS compiler-enforced
  via subtree-internal.
- [Five kinds may prove too few / too many] → closed enum with additive
  contract revision path; kind is metadata for filtering, not storage
  structure — adding one is a one-line change plus spec revision.
- [LadybugDB is young (0.22.x)] → driver seam isolates blast radius;
  `schema.cypher` exercised by open-time round-trip tests.
- [First cgo dependency] → documented; kernel/store stay pure-Go; CI cgo
  job.
- [No embedding producer yet — vector lane dormant] → keyword/graph lanes
  carry day-one value; `E` fixed at open so enabling embeddings later is
  not a migration.
- [RRF weights untuned] → deterministic default; tuning deferred to a
  recall-quality change with real data.

## Migration Plan

Greenfield. First open of `<data-dir>/chitta/` creates the schema
idempotently (DDL guarded by a schema-version node). Rollback = delete the
directory; `zbot.db` untouched.

## Open Questions

- Exact `E` default (384 vs 768) — decided with the ONNX model in the
  embeddings follow-up; knob exists now.
- Whether `Mentions` also links raw messages (not just distilled records)
  — deferred to the distillation change that would produce those links.
- Graph-lane entity-mention extraction from recall queries (exact
  normalized match vs fuzzy) — decided during implementation of D4 with
  fixture data; spec only requires the lane be attributed when it matches.
