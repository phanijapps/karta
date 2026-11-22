# Proposal

## Why

Karta has providers, a model catalog, and a picker — but no conversation.
Chat is still fixtures that reset on reload, and nothing persists under
`~/.zbot/data/` (the constitution's SQLite home is empty). The smallest
real vertical slice is a simple chat: **one persistent session that lives
forever**, messages stored locally, replies from a configured provider
model, with basic built-in tools and no agents or skills. Informs and is
informed by the agentzero inspiration note
(`docs/inspiration/agentzero.md`).

## What Changes

- **SQLite store (new `internal/store`).** `~/.zbot/data/zbot.db` via
  modernc.org/sqlite (pure Go; FTS5/sqlite-vec-capable for memory mode).
  Five append-only tables: `sessions` (the eternal conversation),
  `executions` (tree: root chat now, subagents later), `messages`
  (atomic per-session `seq`, tool fields, provider/model provenance,
  request token usage), `compactions` (context markers scoped to a
  context view), and `timeline_events` (durable observability). Append +
  cursor-paginated replay; no migration of any legacy database.
- **One eternal session.** A single fixed session (`default`) is created
  lazily on first message and never rotated, archived, or deleted — the
  transcript only grows. No session management UI, no multi-session.
- **Simple chat service (new `internal/chat`).** Send: append the user
  message → build a bounded history window (last N messages by `seq`) →
  one OpenAI-compatible chat completion against the chosen
  provider/model → if the model requests built-in tools, execute them
  (hard cap on tool rounds) and complete again → append the assistant
  message. Synchronous request/response; no streaming (follow-up).
- **Basic tools (new `internal/tools`).** A `Tool` seam (name,
  description, parameters, exec) with two builtins — `clock.now` and
  `echo` — wired into the chat request as OpenAI function tools. No
  registry loading, no filesystem/shell tools, no skills.
- **Contract chat endpoints (additive).** `POST /api/chat/messages`
  (text + selected model `provider/model-id` → appended user + assistant
  messages; typed failures reusing provider reasons), and
  `GET /api/chat/messages?after_seq=&limit=` for incremental transcript
  sync. Model must be selected; no default is invented.
- **Durable observability.** Every appended message emits a `session`
  timeline event and every tool execution a `tool_call` event — each
  persisted durably BEFORE volatile notification, replayable by cursor.
  Live SSE delivery stays with the future streaming change.
- **Frontend chat goes live.** Chat.svelte loads the transcript on mount
  (`after_seq` sync), sends via the new endpoint using the picker's
  selected model, shows a pending state while the daemon works, and
  renders tool activity honestly. Mock replies, sample conversations,
  and the search-over-fixtures flow retire. The picker's "Simulated"
  badge becomes live.

**Out of scope (recorded):** streaming/SSE delivery, multiple sessions,
agents/delegation/skills/engine runs, compaction, token budgeting,
registry-loaded tools, message editing/deletion, and any UI beyond the
existing chat surface.

## Capabilities

### New Capabilities

- `simple-chat`: The eternal single-session chat — persistence
  guarantees, message exchange semantics (provider completion with
  bounded tool rounds), cursor replay, and emission behavior.

### Modified Capabilities

- `api-contract`: Adds the chat send + transcript-sync endpoints with
  typed failures (additive).
- `mock-chat`: **Removed** — fixture conversations and deterministic
  replies are replaced by the live simple chat (capability retired;
  `retire_capabilities: true`).
- `desktop-shell`: The offline-preview scenario acknowledges the chat
  surface degrades to an explicit daemon state rather than seeded mocks.

## Impact

- **Backend:** new `internal/store` (SQLite, modernc driver, migrations),
  new `internal/tools`, new `internal/chat`; `internal/providers` gains a
  non-streaming `ChatCompletion` (OpenAI-compatible `/chat/completions`);
  `internal/server` gains two handlers; eventbus wiring in `cmd/zbotd`.
- **Contract & codegen:** additive endpoints/schemas; regeneration.
- **Frontend:** Chat.svelte + session entity rework (transcript sync,
  pending state, tool rendering), `client.ts` chat methods, sample/search
  fixtures retire, Playwright route-mocked coverage.
- **Docs:** `docs/inspiration/agentzero.md` (this change's grounding),
  package AGENTS.md files, product-context, frontend README.
- **Sequencing note:** builds on `add-model-catalog` (picker selection
  feeds the chat request); apply after it.
