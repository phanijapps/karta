# Design

## Context

Goal runs are the engine's only entry point; everything else (chat,
future channels) is a caller. The sidebar's main nav (Conversations,
Code Wards placeholder, Settings) is the workspace surface — schedules
belong there per the operator. The registry owns file-defined control
plane (skills/MCPs CRUD pattern: atomic no-clobber installs, watcher
coexistence). `domain.EventDelegation`-style observability: every
scheduler decision must emit.

## Goals / Non-Goals

**Goals:** cron-fired goal runs with per-schedule sessions; registry CRUD
parity with skills; a real main-area page with live next-fire times;
every fire/skip observable; laptop-friendly missed-fire semantics.

**Non-Goals:** calendar/interval UI beyond cron (natural-language
schedule authoring later); concurrent multi-fire (one in-flight run per
schedule; a fire while running is skipped-and-recorded); distributed
locking (single daemon by design); schedules in Settings.

## Decisions

1. **Schedules are a registry kind** (`config/schedules/*.yaml`, strict
   fields, capacity 128, Kind `"schedules"`) with the skills-style write
   path (Create/Update/Delete, atomic temp+rename, no-clobber, duplicate
   name errors). Alternatives: a dedicated store table (rejected — the
   registry's watch/CRUD/provenance machinery already exists; schedules
   are user-editable files by the same logic as skills).
2. **robfig/cron/v3 with standard 5-field expressions** — the de-facto
   Go parser (second-precision descriptors like `@daily` included via
   its parser). One parser shared by runtime AND the contract's
   validation (invalid expressions are typed 400s, and the frontend
   dialog surfaces the parse error).
3. **One persistent session per schedule** (`sched-<name>`): each fire
   is a RunGoal into the SAME session, so a schedule's whole history —
   fires, steps, compactions — lives in one Timeline the user can open
   from the Schedules page. Alternative: a fresh session per fire
   (rejected — fragments history; the per-schedule session mirrors how
   the eternal chat session works).
4. **Fire semantics**: the runtime ticks every second (cron's natural
   granularity), comparing schedule-second to now; a fire starts
   RunGoal with GoalRequest{SessionID, Goal, Agent, Model}. While a
   schedule's run is in flight, further fires are SKIPPED (recorded as
   `skipped_overlapping`). Missed fires (daemon asleep): `skip` (default)
   forgets them; `catch_up` fires once immediately on wake.
5. **Observability**: fire/miss/skip decisions emit `session` timeline
   events on the schedule's session AND the scheduler keeps the last
   outcome in memory for the list endpoint (next_fire + last_outcome).
   No new contract event kind — runs are already fully observable.
6. **In-memory schedule state only** (last outcome, in-flight map); the
   durable truth is the registry files + the sessions/events the runs
   produce. Restart re-derives everything (missed handling per policy).
7. **Frontend**: Sidebar row (CalendarClock icon) between Conversations
   and Code Wards → Schedules page (feature `schedules/`): card list
   with countdown, create/edit dialog posting to the CRUD contract with
   daemon-side expression validation, delete confirm, "open timeline"
   per card switching to the schedule's session.

## Risks / Trade-offs

- [Cron expression sprawl] — strict 5-field/descriptor parsing only;
  invalid = typed 400 at create; UI shows the parse error inline.
- [Runaway goals on tight crons] — overlap-skip bounds concurrency to 1
  per schedule; step bounds already cap each run.
- [Secrets in goals] — schedules are local files (local-first); no
  credential interpolation v1.
- [Clock changes/DST] — robfig handles time-zone via the local clock;
  schedules carry no TZ field v1 (local time), recorded as a limitation.

## Migration Plan

Additive kind + endpoints + new page. No existing surface changes except
the sidebar gaining one row. Rollback = revert.

## Open Questions

(none — the operator resolved placement: main area, not Settings)
