# Inspiration: agentzero (`/home/videogamer/projects/agentzero/`)

A prior z-Bot-shaped system (Node/TS + Rust workspace, pnpm monorepo):
goal-oriented autonomous agents, self-learning memory, multi-agent
delegation, local SQLite storage, contract-first APIs. Per the root
constitution it supplies **conceptual inspiration only** — schemas,
endpoints, and code are never copied or imported. This note records what
we take and what we deliberately leave for later.

## What we studied

- `stores/zbot-conversation/` — SQLite message persistence (Rust):
  `messages` table with `id (msg-<uuid>)`, `session_id`, `role`
  (user|assistant|tool|system), `content`, `created_at` (RFC3339),
  `token_count`, `tool_calls` (JSON array on assistant turns),
  `tool_call_id` (links `role=tool` rows back to their assistant call),
  and `seq` — a per-session monotonic counter assigned **atomically
  inside the INSERT** (server-side subquery) so concurrent appends cannot
  collide. Replay uses cursor pagination (`after_seq`).
- `stores/zbot-runtime-sqlite/` — pooled SQLite connection + migration
  discipline (schema version constant, `CREATE TABLE IF NOT EXISTS`
  bootstrap).
- `runtime/agent-primitives/tool.rs` — a `Tool` trait (name,
  description, parameters, execute-with-context); tools are plain,
  testable units before any agent loop exists.
- `services/daily-sessions/` — session lifecycle management.
- `contracts/` (OpenAPI + AsyncAPI) — contract-first evolution, same
  philosophy as our `contracts/openapi.yaml`.

## What Karta adopts (simple chat, baby steps)

- The **message row shape** above (minus `execution_id`, which belongs to
  autonomous runs): role/content/timestamp/tokens/tool_calls/tool_call_id.
- **Atomic `seq` + `after_seq` cursor pagination** for incremental
  client sync of an ever-growing transcript.
- **Tools as small named units** with description + parameters — seeded
  with two trivial builtins (clock, echo) so tool plumbing exists before
  agents/skills do.
- Contract-first chat endpoints; local SQLite under `~/.zbot/data/`.

## What Karta deliberately defers or rejects

- Multi-agent orchestration, executions, checkpoints, autonomy tables,
  intent analysis, distillation/memory, knowledge graph — agentzero's
  core; ours arrives via the engine change behind `domain.AgentEngine`.
- **Daily session rotation** — Karta's first chat is ONE persistent
  session that lives forever; rotation is a non-goal.
- The Rust/TS stack and any schema/endpoint copying — conceptual
  inspiration only, per the constitution.

See `openspec/changes/add-simple-chat/` for the change this informed.
