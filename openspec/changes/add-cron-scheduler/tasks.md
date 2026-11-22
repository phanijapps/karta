# Tasks

## 1. Registry + runtime

- [x] 1.1 Registry: schedules kind (strict loader, capacity, hot-reload)
  + CRUD write paths (atomic, no-clobber, duplicate/rename semantics).
  Verify: registry tests mirror the skill suite.
- [x] 1.2 internal/scheduler: cron runtime (robfig/v3, second tick,
  per-schedule sessions, overlap-skip, missed policy, eventbus
  decisions, clean shutdown, next-fire computation). Verify: fake-clock
  unit tests (fire, overlap-skip, catch_up, skip) + race green.
- [x] 1.3 cmd/zbotd wiring: scheduler over the registry + engine.

## 2. Contract + handlers

- [x] 2.1 Contract (GET/POST /api/schedules, GET/PUT/DELETE
  /api/schedules/{name}; list carries next_fire + last_outcome) +
  codegen + server handlers (typed 400 parse errors). Verify: handler
  tests incl. invalid-expression 400s and round trip.

## 3. Frontend

- [x] 3.1 Generated client calls + Schedules page (cards, create/edit
  dialog with parse-error surfacing, delete confirm, open-timeline) +
  sidebar row between Conversations and Code Wards. Verify: Playwright
  flows (roster, invalid expression rejected inline, create→card
  renders) + svelte-check clean.

## 4. Review and hardening

- [x] 4.1 zbot-reviewer read-only; fix P0/P1. Verify: zero open P0/P1.
- [x] 4.2 task verify green. Verify: exits zero.
