# Tasks

## 1. Chitta: markers, seam events, mutex snapshot

- [x] 1.1 Marker seam: `MarkDistilled(sessionID, attempts, deadLetter)` + `DistillationMarkers(limit)` on the driver (ChittaMeta nodes `distilled:<id>`) and System; strict typed errors. Verify: marker round-trip test on both drivers; a marked session is reported back with attempts/dead fields.
- [x] 1.2 Seam observability: `Remember`/`Revise`/`Connect` emit exactly one `ChittaToolInvoked` when `host.Source == distiller` (agent/user path unchanged, still exactly one via Dispatch — no double emission either path). Verify: bus tests — distiller-source seam write emits one event; agent-source seam write emits none (Dispatch owns it); tool path still exactly one.
- [x] 1.3 Store snapshot under mutex: seam entry points (`Dispatch`, `Remember`, `Revise`, `Connect`, `About`, recall pipeline entry) read `s.store` once under `s.mu` and pass the snapshot down (engine-tools review carry-over). Verify: `go test -race` green with a test that closes while a dispatch runs.

## 2. Distiller package

- [x] 2.1 `backend/distiller/` core: Settings re-read each pass (json + prompt file + embedded default), SessionSource + LLMCaller adapters, serial worker loop (ticker), selection (store sessions minus markers), attempts/dead-letter (3). Verify: unit tests with a fake source/LLM/marker set — selection skips marked and dead-lettered; failed extraction increments attempts; third failure dead-letters.
- [x] 2.2 Embedded adapted prompt + strict output validation (schema: observations/entities/relationships with caps + confidence bands; malformed JSON fails the attempt). Verify: golden malformed-output cases fail cleanly; the valid case maps fully (spec scenario "Malformed LLM output fails the attempt").
- [x] 2.3 Mapping + writes: kind/category mapping, tags from keys + epistemic classes, entity-type collapse, episode split (learnings→observations; summary/outcome→log), procedure→kind=procedure JSON content; one Remember batch + Connects with `source: distiller` provenance; MarkDistilled after success. Verify: end-to-end test over a fake chitta system — batch persisted with provenance, relationships connected, transcript text absent (spec scenarios "Distillation writes carry distiller provenance", "Session end alone writes nothing" via unmarked path, "Atomic per-session write" via injected mid-batch failure, "A distilled session is not re-distilled" via second pass).
- [x] 2.4 Idempotency under model swap: re-distill a session after "changing the model" (new LLM stub returning different output) — existing evidence no-ops or converges; no duplicates. Verify: second-pass assertions on the fake system (spec scenario "A distilled update converges instead of overwriting").

## 3. Settings API + config

- [x] 3.1 Contract-first: `GET/PUT /api/distillation` in `contracts/openapi.yaml` (typed errors: unknown provider, empty model), `task codegen` zero drift. Verify: codegen clean; contract lint valid.
- [x] 3.2 Daemon-managed `~/.zbot/config/distillation.json` (provider, model, prompt; defaults seeded on first GET) + `internal/server` handlers (PUT validates provider against configured providers, applies immediately). Verify: handler tests — round-trip, unknown-provider 400 naming it, empty-model 400 (spec scenarios "Round-trip settings", "Unknown provider rejected").

## 4. Frontend: Distillation card

- [x] 4.1 Settings → Advanced: live Distillation card — provider/model picker (from `/api/models`), prompt textarea (daemon-sourced), Save → PUT, inline errors, connection state when down; visually distinct from the remaining read-only previews. Verify: `pnpm check` + build clean; no fixtures rendered for live values (spec scenarios "Model selection persists live", "Prompt round-trips", "Unreachable daemon shows state, not fixtures" — tested via existing offline shell-test harness where feasible).

## 5. Wiring + docs

- [x] 5.1 `cmd/zbotd`: construct the distiller (chitta sys, store source, provider LLM caller, settings, bus) and start it; update `backend/AGENTS.md` (zone rule: plugin-zone composition components may import other plugins' public surfaces), distiller AGENTS.md, chitta AGENTS.md (markers + seam events). Verify: build clean; zone text updated.
- [x] 5.2 Product-context: distillation live (one sentence under Core Concepts). Verify: sentence present; Advanced no longer described as preview-only for distillation.

## 6. Verification

- [x] 6.1 Full: `go test -race` (skillhub excluded), `CHITTA_DRIVER=ladybug` suite incl. marker tests, golangci-lint zero, boundary greps (distiller imports kernel + chitta only; nothing imports distiller but cmd; chitta still imports no store), codegen zero drift. Verify: all green in one run.

## 7. Review and hardening

- [x] 7.1 Run the read-only zbot-reviewer subagent over the change scope; fix every P0/P1; fix P2s or record why not in the change summary.
- [x] 7.2 Re-run `task verify` after review fixes; mark complete only when green.
