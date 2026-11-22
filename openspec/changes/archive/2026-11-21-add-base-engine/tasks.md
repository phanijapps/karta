# Tasks

## 1. Preflight & contract

- [x] 1.1 Preflight eino v0.9.23 agent primitives (ReAct/flow) against the bounded-loop seam; record the loop choice (framework agent vs thin loop over spike-verified Generate) with rationale in the design notes; verify the chosen path compiles against the pinned versions with a fake model
- [x] 1.2 Add the optional qualified `model` field to `GoalRequest` in `contracts/openapi.yaml`; verify `npx @redocly/cli lint contracts/openapi.yaml` passes and `task codegen` regenerates both targets drift-free with `task check:frontend` green

## 2. Engine seams

- [x] 2.1 Create `internal/engine`: the `ToolProvider` seam (list defs, exec by name with JSON args, typed unknown-tool error), the builtin provider wrapping the chat change's builtins, and registry-agent resolution (system prompt incl. prompt-ref, model override, tool allowlist) against the registry snapshot; add the package `AGENTS.md`; verify unit tests cover provider listing/allowlist filtering, prompt-ref resolution, unknown tools, and the no-agent wiring prompt
- [x] 2.2 Extend `internal/store` with run support: session ensure/list/detail (bounded, truncated), execution lifecycle updates (status, error, token rollups, finish), and durable event replay pagination by id; verify store tests cover lifecycle transitions and truncated listing

## 3. Eino adapter

- [x] 3.1 Create `internal/engine/eino` (sole eino/eino-ext importer): construct the chat model from the provider plane (spike-verified config + credential seam), map store rows ↔ schema.Message, implement RunGoal (root execution, goal row, bounded tool loop with intent-before-execution rows, terminal statuses, token rollups) and Cancel (per-session contexts, safe-point checks, idempotent on idle); Resume returns the typed not-yet; add the package `AGENTS.md`; verify adapter tests with a fake ChatModel cover plain run, tool round, step bound, model failure → execution failed, cancel mid-run, no-model refusal, and event emission (durable-first, ordered)
- [x] 3.2 Wire the engine in `cmd/zbotd` (config-selected, store + providers + registry + eventbus) and verify `go build ./...` with the eino boundary check green (eino imports ONLY under internal/engine/eino)

## 4. Daemon surface

- [x] 4.1 Implement the goal/session handlers in `internal/server`: `POST /api/goals` (optional model, registry-agent validation, 201 session, typed no-model refusal), session list (bounded/truncated) and detail from the store, cancel wired to the engine, and resume as 501 typed not-yet; verify handler tests (fake engine seam) cover every api-contract scenario including unknown agent/model refusals
- [x] 4.2 Implement `GET /api/sessions/{id}/events` SSE: durable replay from timeline_events then live bus subscription filtered by session, gap-free handoff (live tail starts at the last replayed id), heartbeats, clean client-disconnect teardown; verify SSE tests using an httptest recorder with flushed writes prove replay-then-live ordering and gap-free handoff
- [x] 4.3 Update `backend/internal/server/AGENTS.md` (engine wiring, SSE semantics); verify `cd backend && go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass

## 5. End-to-end verification & docs

- [x] 5.1 Manually verify against the local ollama on a throwaway home: start a goal with the qualified model via curl, watch the SSE stream show lifecycle/step/tool events (include a `clock.now` round), cancel a slow local-model run mid-flight and confirm the cancelled status and intact rows; record the command log and SSE transcript in the change evidence note
- [x] 5.2 Update `docs/product/product-context.md` and the root README (goal runs are contract-real, engine layer live, UI pending) plus reference `docs/architecture/memory-graph-ladybugdb.md` from the roadmap notes; verify docs claim only what task 5.1 demonstrated
- [x] 5.3 Confirm constitution invariants (eino isolation, boundaries, codegen drift-free) and run `task verify` green end-to-end

## 6. Review and hardening

- [x] 6.1 Run the read-only `zbot-reviewer` subagent over the change scope, change artifacts, and the five constitution boundaries; fix every P0/P1 finding and fix P2s or record in the change summary why not; verify findings are resolved with targeted tests
- [x] 6.2 Re-run `task verify` after the final review fixes and documentation edits; verify it is green with no generated drift or unrelated files before declaring implementation complete

## Workflow follow-up

- Apply after `add-simple-chat` is implemented and archived (store, builtins, and durable events are prerequisites).
- Archive only after the review task is complete: reviewer ran, all P0/P1 findings fixed, and `task verify` green after the final edit.
