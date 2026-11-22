# Proposal

## Why

Karta acts only when asked. A scheduler — cron/timer expressions that start
`RunGoal` runs on their own — turns it into an agent with a calendar: daily
digests, periodic maintenance, watch-and-notify. Phase 1 of
[`docs/product/backlog.md`](../../../docs/product/backlog.md); requires no
new engine concepts (a scheduled run is just a run with a synthetic goal
request).

## What Changes

- A scheduler package under `backend/internal/` that evaluates schedule
  expressions and starts runs through the existing engine seam; schedules
  live in the config tree (`~/.zbot/config/schedules/**`), registry-loaded
  like agents/skills (zero hardcoded schedules).
- Scheduled runs are ordinary Timeline sessions: visible, inspectable,
  cancellable like any run; their triggering schedule is recorded on the
  execution.
- Contract-first CRUD for schedules (create/list/update/delete + upcoming-
  run preview) in `contracts/openapi.yaml`, then handlers; SSE surfaces the
  next-fire times.
- Missed-fire policy (skip vs catch-up) is per-schedule config, defaulting to
  skip (a laptop daemon sleeps; catch-up floods are hostile).
- Every scheduler decision emits to the eventbus (rule 4: no silent work).

## Capabilities

### New Capabilities

- `cron-scheduler`: schedule expressions over goal runs — config, CRUD
  contract, missed-fire policy, observability.

### Modified Capabilities

(none — the engine seam is untouched; the scheduler is a new caller)

## Impact

New `internal/scheduler` package, registry extension (schedules kind),
`contracts/openapi.yaml` + codegen, server handlers, settings UI section
(later change or included — proposal-stage decision). Tests: scheduler
evaluation unit tests with a fake clock; integration through the fake-engine
conformance seams; zero live provider calls.
