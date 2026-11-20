# Simple chat — design review record (pre-implementation)

## gpt-6.1-sol design review

Verdict: **proceed-with-fixes / BLOCK until corrected** — 1 P0, 10 P1,
4 P2, plus deep-research and memory compatibility verdicts. All 16
findings folded into the artifacts (design.md rewritten; specs/proposal/
tasks updated; strict validation green):

- **F1 (P0) durable observability**: `timeline_events` table added;
  events persist BEFORE volatile notify; replay by cursor; SSE delivery
  stays with the streaming change (documented, not silently dropped).
- **F2 page-cursor semantics**: `next_cursor` = last delivered seq (never
  the db tail) + `truncated`; UI catch-up loop pages to the end.
- **F3 post-send reconciliation**: UI re-syncs from the last contiguously
  delivered cursor after EVERY outcome; POST returns all exchange rows
  (failures return the persisted user row + cursor).
- **F4 failure vs append-only**: spec keeps committed intermediate rows;
  tool intent persisted BEFORE execution; no final row on failure.
- **F5 concurrent sends**: per-session send serialization in the service
  (no db transaction over provider calls); two-client test added.
- **F6 exchange-complete windows**: window front trims at exchange
  boundaries (no dangling tool references); boundary tests added.
- **F7 compaction reader defined**: single in-scope summary mapped to
  system, suffix excludes summary rows, bounded with a `needsCompaction`
  signal; D2 uses the accessor; marker/multi-marker/large-suffix tests.
- **F8 append-log vs context-view (the research-mode fix)**: history and
  compaction markers are execution-scoped (root today, inheritance
  policy later); research branches never mix contexts.
- **F9 session identity**: first-class `sessions` row; `default` is not
  a goal-run session — reconciliation documented for the engine change;
  `EnsureSession` means what it says.
- **F10 token semantics**: request usage (nullable, non-summable as
  content size) distinguished from content counting; per-execution
  billing rollups; artifacts aligned; store stays engine-agnostic.
- **F11 chat deadline**: separate configurable chat timeout (default
  180s, spike-justified); discovery keeps 10s; slow-completion test.
- **F12 roadmap reservations (D8)**: checkpoints/artifacts/durable-work/
  range-distillation reserved additively; status TEXT extensible;
  distillation never one-per-session on an eternal conversation.
- **F13 index/test hygiene**: duplicate `(session_id, seq)` index
  dropped (UNIQUE is the index); file-backed WAL concurrency tests.
- **F14 composer rules re-homed**: Shift+Enter/blank/IME/focus
  scenarios moved into simple-chat.
- **F15 provenance**: provider/model persisted on assistant rows,
  surviving restarts.
