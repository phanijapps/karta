# dev-workspace Specification

## Purpose

The buildable Go workspace and unified task runner that every z-Bot change develops and verifies against, from a clean checkout.

## Requirements

### Requirement: Buildable Go workspace

The repository SHALL provide a `go.work` workspace (Go 1.26+ toolchain) linking the `backend/` module, and `go build ./...` SHALL succeed from `backend/` on a clean checkout with no engine-framework dependencies present.

#### Scenario: Clean checkout builds

- **WHEN** a clean checkout is opened and `go build ./...` runs from `backend/`
- **THEN** the build succeeds with only stdlib dependencies resolved

#### Scenario: Backend module is workspace member

- **WHEN** the workspace is inspected
- **THEN** `go.work` lists `backend/` as a `use` directive and no nonexistent module directories

### Requirement: Task runner entrypoints

The repository SHALL provide a `Taskfile.yml` with `build`, `test`, `lint`, `fmt`, and `codegen` tasks mapping to the underlying Go toolchain and contract pipeline.

#### Scenario: Task commands run the toolchain

- **WHEN** `task build`, `task test`, `task lint`, or `task fmt` runs from the repository root
- **THEN** it executes the corresponding Go toolchain command over the backend module and reports its result

#### Scenario: Unknown task fails loudly

- **WHEN** a task name not defined in the Taskfile is invoked
- **THEN** the task runner reports the available tasks instead of silently succeeding

### Requirement: Codegen derives from the contract

The `codegen` task SHALL validate `contracts/openapi.yaml` and regenerate client bindings from it; it SHALL be idempotent and SHALL succeed when `frontend/` does not yet exist (generating only what current targets support).

#### Scenario: Codegen is idempotent

- **WHEN** `task codegen` runs twice in a row with no contract change
- **THEN** the second run produces no diff in generated output

#### Scenario: Frontend absent

- **WHEN** `task codegen` runs while `frontend/` contains only its AGENTS.md
- **THEN** the task succeeds performing contract validation and any backend-side generation, skipping frontend generation
