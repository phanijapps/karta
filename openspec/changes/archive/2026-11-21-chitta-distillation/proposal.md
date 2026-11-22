# Proposal

## Why

The evolution loop's write path is missing: conversations accumulate in
`zbot.db` and nothing turns them into durable memory/knowledge. The
distiller closes the loop — sessions are distilled by a backend job
into chitta through the trusted seam (`source: distiller`), transcripts
never persisted, re-runs idempotent. Step 2 of the close-the-loop arc.

## What Changes

- **Distiller worker** (`backend/distiller/`): background job (ticker +
  session-end trigger) that selects undistilled sessions, runs LLM
  extraction through the existing provider plane, validates the
  structured output, maps it to chitta's contracts, and writes one
  atomic `Remember` batch + `knowledge_connect` calls per session.
- **Distillation markers in chitta**: `MarkDistilled(sessionID,
  attempts)` / `IsDistilled` on the trusted seam (chitta's ChittaMeta
  node) — the distiller's checkpoint lives with the writer; zbot.db
  stays untouched (append-only, conversations only).
- **Seam-write observability**: trusted-seam `Remember`/`Revise`/
  `Connect` calls with `source: distiller` emit one `ChittaToolInvoked`
  event each (tool-path emission unchanged — exactly one per logical
  op, from either path).
- **Prompt**: an embedded default (adapted from the reviewed
  `distillation-prompt.md`: categories→kinds, keys/epistemic→tags,
  entity-type mapping, episode split, generalized procedure steps),
  overridable at `~/.zbot/config/distillation-prompt.md`.
- **Settings → Advanced becomes real for distillation**: daemon-managed
  `~/.zbot/config/distillation.json` (`{provider, model, prompt}`)
  applied without restart; contract-first `GET`/`PUT /api/distillation`;
  a Distillation card in Settings → Advanced (provider/model picker
  from the live catalog + prompt editor). Model swaps are safe:
  contribution identity makes re-distillation idempotent — no data
  migration, unlike embedding dims (which stay config-file-only).
- **Concurrency fix** (review carry-over): `Dispatch`/`Remember`/
  `Revise`/`Connect` snapshot the store under `System.mu` (the
  distiller is a concurrent caller).
- **Zone rule extension**: `backend/distiller` is a plugin-zone
  composition component — it imports kernel seams (store reads, event
  bus, providers) AND the chitta public surface (writes); nothing
  imports it but `cmd`. backend/AGENTS.md zone text updated.
- Out of scope: belief synthesis, contradiction detection, decay
  (graph-accuracy change), embedding work, briefing middleware (next
  change), procedures-as-executable surface.

## Capabilities

### New Capabilities
- `chitta-distillation`: the distill-only write path — selection,
  extraction contract, mapping rules, idempotency, dead-lettering,
  observability, and the no-transcripts guarantee.

### Modified Capabilities
- `settings-shell`: Advanced stops being preview-only — the
  Distillation card is a real, daemon-backed control.
- `api-contract`: `GET`/`PUT /api/distillation` (contract-first).

## Impact

- **Code:** new `backend/distiller/**`; chitta seam + marker methods +
  seam-event emission; `internal/server` handler; contract +
  codegen; frontend Advanced card; `cmd/zbotd` wiring;
  backend/AGENTS.md zone text.
- **Data:** chitta ChittaMeta markers; `~/.zbot/config/distillation.json`
  + optional `distillation-prompt.md` override. zbot.db untouched.
