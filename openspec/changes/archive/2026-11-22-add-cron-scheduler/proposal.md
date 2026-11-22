# Proposal

## Why

Karta acts only when asked. A scheduler — cron expressions that start
`RunGoal` runs on their own — turns it into an agent with a calendar
(Phase 1's last piece; requires no new engine concepts — a scheduled run
is just a run with a synthetic request). Operator direction: the
Schedules surface lives in the MAIN navigation area (sidebar, alongside
Conversations and Code Wards) — not Settings — because schedules are a
first-class workspace object, not a configuration detail.

## What Changes

- **Schedule definitions in the registry**: `~/.zbot/config/schedules/*.yaml`
  (name, cron expression, goal, optional agent, optional model,
  missed-fire policy `skip` default | `catch_up`), loaded like other
  kinds (strict decode, capacity-bounded, hot-reload) with registry CRUD
  write paths (atomic installs, no-clobber, provenance).
- **Scheduler runtime** (`internal/scheduler`): evaluates cron expressions
  (robfig/cron/v3), fires `RunGoal` per schedule into a per-schedule
  persistent session (`sched-<name>` — history and Timeline per
  schedule), records fired/missed decisions, emits every decision to the
  eventbus (constitution rule 4: no silent work), cancellable runs like
  any other, daemon-lifetime goroutine with clean shutdown.
- **Contract (contract-first)**: `GET/POST /api/schedules`,
  `GET/PUT/DELETE /api/schedules/{name}`, list entries carry the next
  fire time and last outcome; strict additive schemas; codegen.
- **Frontend — main area**: a Schedules sidebar row (between
  Conversations and Code Wards) opening the Schedules page: schedule
  cards (name, expression, humanized cadence, next fire countdown, goal,
  agent), create/edit dialog (expression validation feedback from the
  daemon's parse), delete with confirm, last-run link into the
  schedule's session timeline.
- Missed-fire `skip` is the default (a laptop daemon sleeps; catch-up
  floods are hostile); `catch_up` fires once on wake when a fire was
  missed.

## Capabilities

### New Capabilities

- `cron-scheduler`: schedule expressions over goal runs — registry
  definitions, CRUD contract, fire/miss semantics, observability,
  next-fire preview.

### Modified Capabilities

(none — the engine seam is untouched; the scheduler is a new caller)

## Impact

`internal/registry` (schedules kind + write paths), new
`internal/scheduler`, `cmd/zbotd` wiring, `contracts/openapi.yaml` +
codegen, server handlers, frontend sidebar + Schedules page + generated
client calls. New dep: `github.com/robfig/cron/v3`. Local-first: all
state under `~/.zbot/`.
