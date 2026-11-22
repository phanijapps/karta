# Proposal

## Why

A principal-architect/principal-engineer review of `backend/` finds quality debt concentrated in
code organization rather than design: one failing stale test (`internal/skillhub.TestRepoCapabilityExpiryAndBinding`
panics on `main`), the core `domain.AgentEngine` seam (`internal/engine`, 410 LOC) has zero tests,
`internal/engine` lacks the constitution-required `AGENTS.md`, `docs/development/go-rules.md` is an
empty placeholder, and several hand-written files exceed 500 LOC. The architecture itself is sound
(boundaries hold: no eino imports outside `engine/eino`, no channel imports in `internal/`, lint
clean, WAL+busy_timeout on SQLite) — so the right move is a behavior-preserving quality pass now,
before more packages accrete on these seams.

## What Changes

- **Findings ledger**: a critical review of every `backend/` package (bottlenecks, clean-code
  issues, defects) recorded in the change as a prioritized ledger (P0/P1/P2), produced with a
  structured GPT ↔ GLM debate between subagents (`openai` and `zai-coding-cn` models) mediated
  by the implementer.
- **Test-suite green**: fix the stale `skillhub` test that panics because it still ages a repo
  capability entry that `ConsumeRepo` now deletes on replay (test defect; product behavior is
  intended and unchanged).
- **Test gaps closed**: unit tests for `internal/engine` (the seam that every engine swap depends
  on) and `internal/textutil`; a regression test per defect fixed.
- **Behavior-preserving refactors**: only where best practice is not followed — split oversized
  hand-written files (e.g. `registry/mutate.go` 656 LOC, `server/mcpimport.go` 525,
  `skillhub/fetcher.go` 516, `providers/store.go` 499, `domain/memory.go` 546) along existing
  seams, with zero API/contract/SSE changes and `task verify` green after each step.
- **Package docs**: `AGENTS.md` for `internal/engine` (constitution compliance); every touched
  package's `AGENTS.md` reviewed for accuracy.
- **Engineering rules**: populate `docs/development/go-rules.md` with the backend engineering
  rules distilled from the review and the debate (naming, file-size budgets, error/context/
  lock discipline, test floors, review gates).

Assumptions recorded: functionality stays the same — refactors are behavior-preserving; genuine
product-behavior bugs found during review are catalogued in the ledger as follow-up proposals and
are NOT silently fixed here. Two bounded exceptions (debate-arbitrated, phase-2-gate-reviewed):
fixing test/eval-harness defects (the stale skillhub test; the fail-open recalleval harness —
F-20, harness-only, happy path unchanged) and restoring the named intent of a function whose
name promises atomicity (F-22). `contracts/openapi.yaml` is untouched, so no codegen churn.

## Capabilities

### New Capabilities

- `dev-backend-quality`: the durable backend quality floor — engineering rules documented in
  `docs/development/`, per-package `AGENTS.md` coverage, a green `task test:backend` with a test
  floor on the engine seam, and behavior-preserving refactor discipline for future changes.

### Modified Capabilities

(none — no existing spec-level behavior changes; `contracts/openapi.yaml` untouched)

## Impact

- `backend/internal/`: `skillhub` (test fix), `engine` (tests + AGENTS.md), `textutil` (tests),
  targeted file splits in `registry`, `server`, `skillhub`, `providers`, `domain`.
- `docs/development/go-rules.md`: from empty placeholder to the backend engineering rules.
- No contract, frontend, desktop, or data-format impact; no new dependencies.
- Test-suite runtime: `internal/mcpclient` (12.9s) and `recalleval` (3.7s) reviewed for
  parallelism/timeout tightening — capped, not eliminated (probe semantics preserved).
