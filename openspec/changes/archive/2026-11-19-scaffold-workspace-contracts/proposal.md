# Proposal

## Why

z-Bot's Day-1 scaffolding sequence (brief §8) starts with Workspace & Contracts: the Go workspace, task runner, OpenAPI contract, pure domain layer, and eventbus that every later change builds on. Contract-first rules mean the API contract must exist before any handler; engine-agnostic rules mean the domain seams must exist before any engine adapter. This is that foundation.

## What Changes

- Add **`go.work`** (Go 1.26+) linking the `backend/` module (the only module for now — `desktop/` joins in the Day-1 frontend/desktop change).
- Add **`backend/go.mod`** with the `zbot/backend` module and minimal deps (chi comes with the server change; zero engine deps now).
- Add **`backend/internal/domain/`** — pure types & interfaces only, zero internal imports: `AgentEngine`, `Compactor`, `HookRunner`, `Channel`, `EventBus` seams; `GoalRequest`, `UserInput`, `Message`, `TokenBudget`, `ContextState`, `CompactionStats`, `HookEvent`, `HookDecision`, `TimelineEvent` (+ event kinds incl. `CompactionTriggered`), `Session` identity types.
- Add **`backend/internal/eventbus/`** — in-process pub/sub implementing `domain.EventBus`: topic-scoped publish/subscribe for timeline events, non-blocking broadcast, subscriber isolation (one slow subscriber cannot drop events for others), context-aware cancellation.
- Add **`contracts/openapi.yaml`** — initial daemon HTTP/SSE contract: health, goal submission (`RunGoal`), session cancel/resume, session list/detail, and the SSE timeline stream with the `TimelineEvent` schema. Strict OpenAPI 3.1 discipline (operationIds, `additionalProperties: false`, bounded identifiers, explicit truncation). Inspired in style only by an unrelated local project's specs; all content derives from the brief.
- Add **`Taskfile.yml`** — `build`, `test`, `lint`, `fmt`, `codegen` (contract → generated clients; wired to real generators, tolerant of the not-yet-existing `frontend/`), `run` (zbotd placeholder deferred until the daemon entrypoint change).

Out of scope: `cmd/` entrypoints, `internal/server/` handlers, config/registry, compaction, hooks, engine adapter, memory, frontend, desktop — each arrives in its own Day-1 change.

## Capabilities

### New Capabilities

- `dev-workspace`: The buildable Go workspace and task runner — `go.work` linking backend, Taskfile entrypoints (`build`/`test`/`lint`/`fmt`/`codegen`) that verify and generate from a clean checkout.
- `domain-model`: The pure domain layer — the core types and interface seams (`AgentEngine`, `Compactor`, `HookRunner`, `EventBus`, `Channel`) that decouple the daemon from any engine, with zero internal imports by construction.
- `eventbus`: The in-process publish/subscribe bus for timeline events — topic-scoped, non-blocking, subscriber-isolated delivery that later persists to the Timeline Tree and streams over SSE.
- `api-contract`: The initial daemon HTTP/SSE OpenAPI contract — health, goal, session, and timeline-stream endpoints with strict schema discipline, as the single source of truth all clients and handlers are generated against.

### Modified Capabilities

None — these four capabilities are new; existing specs (`agent-conventions`, `dev-agent-tooling`, `product-context`) encode rules this change follows, not requirements it alters.

## Impact

- **New files:** `go.work`, `Taskfile.yml`, `contracts/openapi.yaml`, `backend/go.mod`, `backend/internal/domain/*.go` (+ tests), `backend/internal/eventbus/*.go` (+ tests).
- **No runtime behavior yet** — no daemon entrypoint exists; the contract and seams are consumed by later changes.
- **Tooling:** Go 1.26+ toolchain and Task (taskfile.dev) become required for development; codegen task references generators but tolerates absent `frontend/`.
- **Downstream:** the engine adapter, server, compaction, hooks, memory, CLI, and frontend changes all consume `domain` types and the `contracts/openapi.yaml` introduced here.
