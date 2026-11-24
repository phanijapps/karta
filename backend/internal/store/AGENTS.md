# store

Karta's local SQLite database at `~/.zbot/data/zbot.db` (WAL mode,
modernc.org/sqlite — pure Go, no cgo). Five append-only tables: sessions
(the session model: one `chat`-type default quick chat + many
`research` sessions, each with a type set at creation and a mutable
ward binding), executions (tree: root chat, subagents
later), messages (one row per turn with execution linkage, tool fields,
provenance, request token usage, atomic per-session seq), compactions
(context markers scoped to an execution), and timeline_events (durable
observability, persisted before volatile notification).

- Nothing here rewrites or deletes rows — append-only by design.
- `Append` assigns per-session `seq` atomically inside the INSERT (the
  agentzero trick); `Replay` is cursor-paginated by seq.
- `HistoryWindow` is the compaction-aware, exchange-complete provider
  context view (latest in-scope marker + suffix, front-trimmed at tool
  exchange boundaries, `NeedsCompaction` signal). Display reads use
  `Replay`; only provider-facing context uses `HistoryWindow`.
- `EnsureSession` idempotently creates the session row (typed at
  CREATION only — the type is immutable) and root execution.
  `SetSessionWard`/`SetSessionTitle` rebind/rename; `ListSessions`
  orders by recent activity (the sidebar's ordering) and exposes
  type + ward for grouping.
- Imports only stdlib + modernc.org/sqlite; sits below chat and server.

- `SetSessionTitleIfUntitled` (sessions.go) is the auto-titling write:
  a conditional UPDATE that never overwrites a user title.
