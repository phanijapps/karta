# Design

## Context

No storage exists under `~/.zbot/data/` yet; chat is fixture-backed and
resets on reload. Providers, model catalog, and picker are live
(`add-openai-compatible-providers`, `manage-model-providers`,
`add-model-catalog`). The agentzero inspiration note
(`docs/inspiration/agentzero.md`) and the Eino verification
(`docs/architecture/eino-verification.md`) ground the message shape,
atomic `seq`, cursor replay, caller-owned history, and tools-as-units.
User decisions: baby steps — one eternal session, message persistence,
provider replies, basic tools, no agents/skills; roadmap context: deep
research mode (subagent trees, artifacts, resumability) and memory mode
(distillation, semantic recall) must not be blocked. A gpt-6.1-sol design
review (16 findings) is folded in throughout; evidence file records it.

## Goals / Non-Goals

**Goals:**

- One persistent chat session (fixed identity, lazy creation, never
  rotated; a future user clear is a boundary marker, never deletes),
  transcript in local SQLite surviving restarts.
- Send = user row + provider completion + assistant row, with bounded
  built-in tool rounds, synchronous.
- Observable durably from day one (events persist before notify) and
  honest in the UI.

**Non-Goals:**

- Streaming/SSE delivery (events are persisted now; live SSE streaming
  lands with the sessions/streaming change), multiple conversations,
  agents/delegation/skills/engine runs, compaction logic, token
  budgeting, registry tools, message editing/deletion, context-panel
  rework, checkpoints/artifacts/durable-work (reserved in D8, not built).

## Decisions

### D0: Eino verified; store refined

The spike proved eino v0.9.23 + its OpenAI-compatible model work against
our provider plane (custom base URL, full tool-call round, per-request
usage), and that eino has NO history/memory — conversations are the
caller's job. This slice needs no eino (the providers client suffices);
the engine change later wraps the verified mechanics, projecting rows to
`schema.Message` inside `internal/engine/eino/` only.

### D1: `internal/store` — five tables, append-only, scoped contexts

modernc.org/sqlite (pure Go; its Linux build ships FTS5 and a `vec`
package — the memory-mode promise is reachable on the same driver).
WAL mode, one writer connection, `busy_timeout`, short write
transactions. Design constraints served: ONE eternal session cleared
only on explicit user command (a clear is a compaction-style boundary —
append-only history survives underneath), compaction-ready context
handling (logic later, storage + bounded reader now), execution-tree
tracking for future subagents, durable timeline events, and the
append-log vs context-view separation the research roadmap requires.

```sql
-- First-class conversation (the eternal chat today; engine goal-sessions
-- later get their own session rows — see D9).
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,          -- 'default'
    title       TEXT,
    created_at  TEXT NOT NULL,
    archived_at TEXT                       -- NULL = live; never set for default
);

-- Execution tree: root chat execution now; subagent executions later.
CREATE TABLE IF NOT EXISTS executions (
    id          TEXT PRIMARY KEY,          -- exec-<uuid>
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    parent_id   TEXT REFERENCES executions(id),
    agent       TEXT NOT NULL DEFAULT 'default',
    task        TEXT,
    status      TEXT NOT NULL,             -- extensible TEXT: running|completed|failed|cancelled today
    delegation  TEXT NOT NULL DEFAULT 'root', -- root|subagent (predecessor lesson)
    error       TEXT,
    tokens_in   INTEGER NOT NULL DEFAULT 0,   -- REQUEST-usage rollups (see D10)
    tokens_out  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_executions_session ON executions(session_id);
CREATE INDEX IF NOT EXISTS idx_executions_parent  ON executions(parent_id);

-- One row per conversation turn (append-only; never rewritten or deleted).
CREATE TABLE IF NOT EXISTS messages (
    id                TEXT PRIMARY KEY,    -- msg-<uuid> — durable identity for index provenance
    session_id        TEXT NOT NULL REFERENCES sessions(id),
    execution_id      TEXT NOT NULL REFERENCES executions(id),
    seq               INTEGER NOT NULL,    -- per-session monotonic, atomic in the INSERT
    role              TEXT NOT NULL,       -- user|assistant|tool|system|summary
    content           TEXT NOT NULL,
    tool_calls        TEXT,                -- JSON array (assistant intent, persisted BEFORE execution)
    tool_call_id      TEXT,                -- tool rows link back to their assistant call
    tool_name         TEXT,
    provider          TEXT,                -- provenance on assistant rows (which provider answered)
    model             TEXT,                -- provenance on assistant rows
    prompt_tokens     INTEGER,             -- REQUEST usage (nullable; NOT content size — D10)
    completion_tokens INTEGER,             -- REQUEST usage (nullable)
    created_at        TEXT NOT NULL,
    UNIQUE(session_id, seq)                -- the uniqueness index IS the replay index (no duplicate)
);
CREATE INDEX IF NOT EXISTS idx_messages_execution ON messages(execution_id);

-- Compaction/context boundaries, scoped to a context view (NULL = root).
CREATE TABLE IF NOT EXISTS compactions (
    id                 TEXT PRIMARY KEY,
    session_id         TEXT NOT NULL REFERENCES sessions(id),
    execution_id       TEXT REFERENCES executions(id),  -- context scope; NULL = root context
    base_seq           INTEGER NOT NULL,   -- summary covers messages with seq <= base_seq in scope
    summary_message_id TEXT NOT NULL REFERENCES messages(id),
    tokens_before      INTEGER NOT NULL,
    tokens_after       INTEGER NOT NULL,
    created_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compactions_scope ON compactions(session_id, execution_id, base_seq);

-- Durable timeline events (written BEFORE the volatile bus publishes —
-- the constitution's "persists to the Timeline Tree" is satisfied by
-- storage now; live SSE delivery lands with the streaming change).
CREATE TABLE IF NOT EXISTS timeline_events (
    id          TEXT PRIMARY KEY,          -- evt-<uuid>
    session_id  TEXT NOT NULL,
    execution_id TEXT,
    parent_id   TEXT,                      -- event ancestry
    kind        TEXT NOT NULL,             -- domain.TimelineEventKind values
    payload     TEXT NOT NULL,             -- JSON kind-specific payload
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session ON timeline_events(session_id, created_at);
```

Store API: `EnsureSession` (idempotent session row + root execution),
`Append(msg)` (atomic `seq` via INSERT subquery), `Replay(session,
afterSeq, limit)`, `HistoryWindow(scope)`, `AppendEvent` + `Events(session,
afterID, limit)`. Provenance columns on assistant rows survive restarts
(the volatile event's agent field does not).

### D2: Chat flow — serialized sends, exchange-complete windows

`chat.Service.Send(ctx, text, providerName, modelID)`, serialized per
session by an in-process send mutex (one exchange at a time; the
composer's disable is UX, not the guarantee — two clients queue, never
interleave). NO database transaction spans a provider call.

1. Append the user row (persisted before any network call).
2. `HistoryWindow(root scope)` (D3): summary marker (mapped to a system
   message for the provider) + suffix rows, trimmed to the exchange-safe
   bound.
3. `providers.Client.ChatCompletion(...)` — separate generous chat
   deadline (configurable, default 180s; the spike proved local 32B
   models exceed 60s — discovery keeps its 10s bound).
4. Tool rounds (cap 2): persist the assistant tool-call INTENT row
   BEFORE executing; execute via `internal/tools`; append tool result
   rows; unknown tool → tool row with an error result the model sees;
   final round completes without tools.
5. Append the final assistant row (with provider/model provenance and
   request usage); return the exchange.

Failure at any step after the user row: return the typed failure; the
user row AND every already-committed intermediate row remain (append-only
makes "no partial assistant row" impossible once a tool round committed —
the spec says exactly this); no final-answer row is appended.

### D3: HistoryWindow — the context view, separate from the append log

The append log is session-wide; the CONTEXT view is execution-scoped
(root today, per-subagent later with an inheritance policy). Reader
contract: the latest compaction marker in scope → its summary row
(mapped to system for the provider) + rows with `seq > base_seq` in
scope, EXCLUDING all role=summary rows from the suffix; the suffix is
trimmed from the FRONT only at exchange boundaries — a window never
starts with a tool row whose assistant call is outside it (dangling
`tool_call_id` reference ⇒ drop leading tool rows; incomplete assistant
tool_calls group at the head ⇒ drop it). If the untrimmed suffix exceeds
the token budget (N rows now), the service returns `needsCompaction`
signal alongside the trimmed window — the compaction change consumes it.
Display reads use plain `Replay`; only provider-facing context uses
`HistoryWindow`.

### D4: Contract — page cursors, reconciliation, request-usage fields

`POST /api/chat/messages {text, model}` (qualified `provider/model-id`,
split daemon-side) → `{messages: [every row appended by this exchange,
in order]}` (user row, any tool rows, final assistant row) plus the
exchange's end cursor; typed failures return the persisted user row and
its cursor so the UI can reconcile deterministically.
`GET /api/chat/messages?after_seq=&limit=` → `{messages, next_cursor,
truncated}` where `next_cursor` is the LAST DELIVERED seq (a page cursor
— never the database tail) and `truncated` means more replay rows
remain. ChatMessage: id, seq, role, content, created_at, tool_calls,
tool_call_id, tool_name, provider, model, prompt_tokens,
completion_tokens (request usage, nullable — D10). The UI's catch-up
loop pages until `truncated=false` on first load, and after EVERY send
outcome (success or failure) re-syncs from its last contiguously
delivered cursor, merging by stable message id — intermediate tool rows
and failure-persisted user rows always become visible. Cross-origin
guard applies.

### D5: Durable events, then notify

Every appended message → `session` timeline event; every tool execution
→ `tool_call` event; both written to `timeline_events` FIRST, then
published to the volatile bus for any live subscriber. The durable write
is the constitution's persistence; replay (`Events(session, afterID)`)
enables the future SSE endpoint to replay-then-live without a schema
change. Domain types only — no eino/generated imports in the store.

### D6: Frontend — catch-up sync, reconciliation, honest pending

Chat.svelte mounts → GET pages until `truncated=false` (cursor held).
Send → composer disabled + pending indicator (no optimistic fakes); on
ANY outcome, reconcile from the last synced cursor (merge by id) — tool
rows render as compact tool lines, failure-persisted user rows appear
with the typed failure surfaced. Daemon-down → explicit state. Sample
conversations, search-over-samples, mock replies, and New Chat retire;
the picker's "Simulated" badge goes live. `client.ts` gains typed
`sendChatMessage` + `syncChatMessages`.

### D7: Tests stay hermetic

Store: file-backed WAL concurrency tests (busy handling, seq uniqueness)
plus `:memory:` unit cases — the race detector does not prove SQLite
lock behavior. Chat: `ChatCompleter` seam with scripted completions
(plain, tool round, round cap, failure-after-tool-round, slow completion
under the chat deadline); two-client serialization test; HistoryWindow
boundary tests (cutoff through a tool group, marker after range, large
suffix). Server: httptest + fakes. Frontend: route-mocked flows incl.
multi-page catch-up and post-failure reconciliation. Manual E2E vs local
ollama recorded in the evidence note.

### D8: Reserved for the roadmap (additive, not built here)

Deep research: versioned checkpoints keyed by execution (replay/context
boundaries, pending tool state — NOT inline columns), artifact metadata
keyed by execution/message (file refs, citations), durable work items
with dedupe+leases for resumability, execution-scoped logs/plans.
Memory: range-based distillation progress `(session, from_seq,
through_seq, pipeline_version)` — never one-run-per-session (the eternal
session never ends), and compaction markers are NOT distillation
progress. FTS5/sqlite-vec/graph tables reference durable message ids
(never rowid). All reference existing session/execution/message ids —
no migration pain. Execution `status` stays unconstrained TEXT so
`queued|waiting|paused` extend it without rebuilds.

### D9: Session identity reconciliation

The eternal `default` conversation is a first-class session row but is
NOT a goal-run session of the existing `/api/sessions` contract (that
contract describes engine-run sessions with statuses/cancel/resume; the
engine change reconciles — goal runs get fresh session ids and never
terminate `default`). Documented now so the next change inherits a
decision, not a collision. `EnsureSession` ensures what its name says.

### D10: Token semantics — content size vs request usage

`prompt_tokens`/`completion_tokens` are REQUEST usage from the
completion response (nullable, unknown=absent): they bill the whole
request context, are NOT summable as content size, and roll up per
execution for billing. Content-size counting (tokenizer estimates for
arbitrary windows) is the future compaction/budgeting change's job; the
columns leave room without conflating the two. `domain.Message.Tokens`
remains content-count semantics; store types stay engine-agnostic.

## Risks / Trade-offs

- [Sync send slow on big local models] → accepted; generous configurable
  chat deadline (spike-proven); streaming SSE is the follow-up.
- [Eternal transcript grows unbounded] → append-only by design; replay
  paginated; HistoryWindow bounded + compaction-ready; `needsCompaction`
  signal reserved.
- [Provider-varying tool JSON] → tolerant decoding, typed malformed
  failure; ollama/z.ai verified in E2E.
- [modernc sqlite] → FTS5/vec verified present in-source; file-backed
  WAL tests cover lock behavior; the store seam keeps a driver swap
  local.

## Migration Plan

Additive contract; fresh `~/.zbot/data/zbot.db` (the predecessor's
`~/Documents/zbot/data/conversations.db` is not read, imported, or
modified — inspected read-only for lessons only). Rollback = revert; the
db file is inert without the daemon code.
