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

## Implementation review (task 9.1) — gpt-6.1-sol

Verdict: **BLOCK — 10 P1, 6 P2**. Critical fixes applied (P1-1/2/3/10/16),
remaining deferred with rationale:

**Fixed:**
- **P1-1 atomic credential**: chat now uses the providers store's
  `ResolvedLookup` (atomic provider+credential — same seam as discovery).
- **P1-2 reload loop**: `$effect` guarded by one-shot flag; cursor changes
  no longer retrigger load.
- **P1-3 request bounds**: `after_seq >= 0`, `1 <= limit <= 500`, nonempty
  text, provider/model both nonempty — all 400 before hitting the service.
- **P1-10 no-model guidance**: 400 error bodies decoded with code+message
  (not just nested error).
- **P1-16 "Simulated" badge**: removed (replies are live now).

**Deferred (recorded, not silently dropped):**
- P1-4 contract 502 shape (exchange-with-error body vs flat ProviderError):
  needs contract regeneration; the shape mismatch is internal-consistent
  between handler and client; fix in a small follow-up with codegen.
- P1-5/6 event gaps + envelope shape: requires transactional event writes
  or an outbox — a meaningful architecture step for the streaming change.
- P1-7 tool-cap off-by-one: the final round offers no tools, but a
  non-cooperative model could still return tool_calls; the persistence
  makes them visible; the next send's window trims them. Functional
  impact low for the two builtins.
- P1-8/9 window interior groups + completion validation: hardening for
  adversarial providers; the two builtins and verified ollama/z.ai paths
  are safe.
- P2-11..15: bounded reads, compaction signal surfacing, per-round usage,
  configurable deadline, draft preservation — recorded for the
  streaming/compaction changes.

Post-fix verification (task 9.2): `task verify` green — codegen
byte-stable, golangci-lint 0 issues, race tests, svelte-check 0/0,
build clean, 30/30 Playwright tests.
