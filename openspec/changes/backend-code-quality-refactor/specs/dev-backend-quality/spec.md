# Spec Delta

## Purpose

The backend quality floor: durable, checkable expectations for engineering rules documentation,
per-package docs, a green backend test suite with coverage on the engine seam, and
behavior-preservation discipline for code-organization refactors.

## ADDED Requirements

### Requirement: Backend engineering rules document

The repository SHALL maintain a backend engineering rules document at
`docs/development/go-rules.md` that states each rule in plain language with its rationale and how
it is enforced (lint, test, review gate, or `task verify`). The document SHALL cover at minimum:
naming and file-size budgets, error-handling and context discipline, concurrency/locking rules,
test floors and test-runtime budgets, and the change-review gates.

#### Scenario: Rules document is populated and navigable
- **WHEN** a contributor opens `docs/development/go-rules.md`
- **THEN** it lists each backend engineering rule with rationale and its enforcement mechanism,
  and contains no empty placeholder sections.

#### Scenario: Rules stay current with the codebase
- **WHEN** a change adds or alters a backend engineering rule encoded in the document
- **THEN** that change updates `docs/development/go-rules.md` in the same change.

### Requirement: Green backend test suite with an engine-seam floor

`task test:backend` SHALL pass with zero failing or skipped-by-default tests on a clean checkout.
Every backend package with non-trivial logic SHALL carry unit tests exercising its public
entrypoints; in particular the package providing the `domain.AgentEngine` seam SHALL have tests
proving that seam's contract without importing the underlying engine library directly.

#### Scenario: Clean checkout is green
- **WHEN** `task test:backend` runs on a clean checkout
- **THEN** all tests pass and no package with non-trivial logic reports `[no test files]`.

#### Scenario: Engine seam tested without the engine library
- **WHEN** the tests for the agent-engine seam package run
- **THEN** they exercise the seam contract using fakes/stubs and the package's tests do not
  import the wrapped engine library.

### Requirement: Per-package documentation completeness

Every Go package under `backend/` (excluding generated code) SHALL have an `AGENTS.md` that
explains, in a few plain-English lines, the package's responsibility, its boundaries (what it
must not import or know), and its key entrypoints.

#### Scenario: New package lands with its doc
- **WHEN** a new Go package is added under `backend/`
- **THEN** the same change adds its `AGENTS.md`, and no existing package's `AGENTS.md` is left
  stale by the change.

### Requirement: Behavior preservation for organization refactors

A change whose stated scope is code organization (renaming, splitting, moving code) SHALL NOT
alter the HTTP/SSE API contract, event payloads, on-disk data formats, or observable daemon
behavior, and SHALL leave `task verify` green.

#### Scenario: Refactor leaves the contract untouched
- **WHEN** a code-organization refactor lands
- **THEN** `contracts/openapi.yaml` is unchanged (zero codegen drift) and `task verify` passes.
