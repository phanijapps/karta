# Design

## Context

Empty Go repository (constitution + agent tooling only). This change executes Day-1 step 1 of the brief: workspace, tasks, contract, domain, eventbus — the foundation the engine adapter, server, and clients consume. The brief mandates a single `contracts/openapi.yaml`; an unrelated local project (`agentzero`) has OpenAPI specs reviewed for style inspiration only (strictness, operationIds, explicit truncation) — zero content copied, no dependency introduced.

## Goals / Non-Goals

**Goals:**
- Clean-checkout reproducibility: workspace builds, tasks run, contract validates with minimal toolchain (Go 1.26+, Task).
- Seams before consumers: domain types compiled and tested with fakes, not mocked into an engine that doesn't exist yet.
- Eventbus correct under concurrency from day one (race-detector clean).

**Non-Goals:**
- No `cmd/` entrypoints, no HTTP server, no engine, no persistence — later Day-1 changes.
- No generated Go server code yet (arrives with the server change); no frontend generation (frontend doesn't exist).
- No contract linting CI beyond the codegen task's validation step.

## Decisions

### D1: Single `contracts/openapi.yaml` (not agentzero's multi-file split)

The brief mandates one file as the single source of truth; agentzero splits per domain plus AsyncAPI. Single file keeps tooling trivial at this size; internal discipline (ordered sections: health → goals → sessions → stream → schemas) substitutes for file-level separation. If the contract outgrows a file, splitting is a later, additive-tooling decision. Adopted from agentzero as *style only*: OpenAPI 3.1, unique operationIds, `additionalProperties: false`, bounded IDs (`maxLength` + pattern), explicit truncation flags, typed error responses shared via components.

### D2: SSE modeled in OpenAPI, not AsyncAPI

z-Bot has exactly one streaming surface (timeline events) consumed by three clients over local HTTP. Modeling it as an OpenAPI `text/event-stream` response keeps one contract and one codegen path; AsyncAPI adds a toolchain for no second use case. Event payload schema mirrors domain `TimelineEvent` kinds.

### D3: Domain purity enforced by a unit test, not convention alone

A test in `internal/domain` walks the package's imports (via `go list -deps` or parsing `go list -json`) and fails on any internal or third-party dependency. Cheap, runs in `go test`, makes the constitution's purity rule mechanically checkable from day one — the mechanical-enforcement risk noted in the dev-environment design now has its first enforcement point.

### D4: Eventbus = per-subscriber bounded channels, drop-oldest overflow

Each subscription is a goroutine-free relay into a bounded channel (default 256). Publish is always non-blocking: full buffer drops the oldest event and increments that subscription's overflow counter; a `Subscription` wrapper exposes `OverflowCount()` so loss is observable (spec's explicit-overflow requirement). No cross-subscriber locks on the hot path beyond the topic map. Alternative — unbounded queues — rejected: a stuck SSE client would grow memory forever; backpressure-into-publisher — rejected: one slow client must never stall an engine run (spec: non-blocking publish).

### D5: Codegen validates now, generates per-target when targets exist

`task codegen` = (1) validate `contracts/openapi.yaml` (always, via `npx @redocly/cli lint` — recorded deviation: the original "`go run` validator" plan had no go-runnable OpenAPI 3.1 validator that avoids adding backend module deps, which task 1.2 forbids), (2) generate frontend TS client only if `frontend/package.json` exists (skipped today), (3) Go server types arrive with the server change, which owns generator choice (`oapi-codegen`). Idempotence: generation is deterministic from the contract; validation emits nothing. This keeps the task honest — it fails on an invalid contract today, and grows generation as consumers land.

### D6: `go.work` links only `backend/`

`desktop/` joins the workspace in the Day-1 frontend/desktop change via one added `use` directive. No placeholder directories — the constitution's repo map already frames these as targets.

### D7: Domain file layout mirrors seams

`domain/engine.go` (AgentEngine, GoalRequest, UserInput), `domain/compaction.go`, `domain/hooks.go`, `domain/event.go` (TimelineEvent + kinds), `domain/bus.go` (EventBus), `domain/channel.go`, `domain/session.go`. One seam per file keeps the map obvious and future imports minimal.

## Risks / Trade-offs

- [Hand-mirrored domain ↔ contract schemas can drift] → The server change adds an alignment test (round-trip marshaling); until then the contract's "mirrors the domain" scenario is the review check.
- [Drop-oldest loses events silently if nobody checks the counter] → Counter exposed on Subscription; the future SSE bridge logs overflow; documented in package docs.
- [Task + Go 1.26 toolchain prerequisites] → Documented in Taskfile header; `go` alone still builds/tests everything (tasks are thin wrappers).
- [Contract written before any server exists] → Intentional (contract-first); endpoints are validated for shape now and gain runtime proof with the server change.

## Migration Plan

New files only; no migration. Rollback = delete `go.work`, `Taskfile.yml`, `contracts/`, `backend/go.mod`, `backend/internal/{domain,eventbus}/`.

## Open Questions

- Default subscription buffer size (256) and its configurability point (bus constructor option vs config plane) — deferrable to the server/SSE bridge change without affecting these specs.
