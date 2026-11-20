# Tasks

## 1. Workspace

- [x] 1.1 Create `go.work` (Go 1.26+ toolchain directive, `use ./backend`) and `backend/go.mod` (module `zbot/backend`, Go 1.26); verify `cd backend && go build ./...` succeeds on a tree with no packages yet, and `go work sync` is clean
- [x] 1.2 Verify zero external dependencies: `cd backend && go mod tidy` changes nothing and `go list -m all` shows only the toolchain/stdlib baseline

## 2. Domain layer (backend/internal/domain/)

- [x] 2.1 Create `domain/engine.go` (AgentEngine: Name/RunGoal/Resume/Cancel; GoalRequest, UserInput) and `domain/session.go` (Session identity types); verify a compile-only fake implementing AgentEngine in a `_test.go` builds and satisfies the interface
- [x] 2.2 Create `domain/compaction.go` (Compactor, Message, TokenBudget, ContextState, CompactionStats) and `domain/hooks.go` (HookRunner, HookEvent, HookDecision with allow/block/warn outcomes); verify table-driven tests construct each type and a fake Compactor/HookRunner satisfies each seam
- [x] 2.3 Create `domain/event.go` (TimelineEvent with session identity, kind, timestamp, payloads; kinds incl. agent step, delegation, tool call, hook decision, CompactionTriggered with tokens before/after + strategy), `domain/bus.go` (EventBus), `domain/channel.go` (Channel); verify a test constructs a CompactionTriggered event with stats and a fake Bus/Channel satisfies each seam
- [x] 2.4 Add the domain purity test (fails on any internal or third-party import in `internal/domain`, via `go list`); verify `go test ./internal/domain/...` passes and fails as expected when a violation is temporarily simulated
- [x] 2.5 Run `gofmt -l`, `go vet ./...`, and `go test ./...` from `backend/`; verify all clean/green

## 3. Eventbus (backend/internal/eventbus/)

- [x] 3.1 Implement the bus (topic-scoped subscribe by session ID, non-blocking publish, per-subscription bounded channel default 256, drop-oldest with observable `OverflowCount()`, context cancellation closing the channel, `Close()` stopping delivery) implementing `domain.EventBus`; verify it compiles as a `domain.EventBus` without a type assertion
- [x] 3.2 Write table-driven + concurrency tests: topic isolation (A-subscriber gets no B events), publish-without-subscribers succeeds, slow-subscriber does not stall publisher, healthy subscriber unaffected by overflowing one, overflow counter observable, context cancel closes channel, Close stops all, `-race` clean; verify `go test -race ./internal/eventbus/...` passes

## 4. Contract (contracts/openapi.yaml)

- [x] 4.1 Author the OpenAPI 3.1 skeleton (info with title/version, servers on localhost, strict component conventions: `additionalProperties: false`, bounded IDs with pattern, shared error response schemas); verify an OpenAPI 3.1 validator accepts it
- [x] 4.2 Define paths: health (liveness), goal run (201/400), session cancel and resume (200/404 typed errors), session list (bounded, explicit truncation flag), session detail (200/404), session events SSE stream (`text/event-stream`); verify every operation has a unique operationId and error bodies reference shared components
- [x] 4.3 Define schemas: TimelineEvent (session id, kind, timestamp, payload variants incl. CompactionTriggered with tokens_before/tokens_after/strategy), Session, GoalRequest, error shapes; verify the SSE payload schema covers every domain TimelineEvent kind
- [x] 4.4 Verify contract↔domain alignment by review: each domain kind mappable to a contract schema variant; record the check in the change summary

## 5. Taskfile

- [x] 5.1 Create `Taskfile.yml` with `build`, `test`, `lint` (go vet + gofmt check), `fmt`, and `codegen` (validate contract always; frontend TS generation only when `frontend/package.json` exists) per design D5; verify each task runs from the repo root and `task codegen` is idempotent (no diff on rerun)
- [x] 5.2 Verify the full loop from a clean state: `go clean -testcache && task build && task test && task lint` all pass

## 6. Integration checks

- [x] 6.1 Constitution boundary check: no package imports `github.com/cloudwego/eino`; `internal/domain` imports nothing internal (grep + purity test); no handler code exists (contract-first holds — no server yet); verify with the boundary greps the `zbot-reviewer` agent uses
- [x] 6.2 Run `openspec validate scaffold-workspace-contracts --strict` and verify it passes

## Workflow follow-up

- Review, then archive the change (syncs 4 new capability specs into `openspec/specs/`).
