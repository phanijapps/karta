# store

Karta's local SQLite database at `~/.zbot/data/zbot.db` (WAL mode,
modernc.org/sqlite — pure Go, no cgo). Five append-only tables: sessions
(the eternal conversation), executions (tree: root chat, subagents
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
- `EnsureSession` idempotently creates the session row and root
  execution ('default' for the eternal chat).
- Imports only stdlib + modernc.org/sqlite; sits below chat and server.
