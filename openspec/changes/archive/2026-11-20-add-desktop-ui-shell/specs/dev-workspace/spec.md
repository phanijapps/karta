# Spec Delta

## ADDED Requirements

### Requirement: Frontend development and static build

The repository SHALL provide documented frontend install and development commands, `task dev`, and `task check:frontend`. Development SHALL use port 9245 unless `WAILS_VITE_PORT` overrides it, and fail if that port is occupied. Frontend checks SHALL include type/component validation and a production SPA build into `frontend/dist` without requiring the daemon.

#### Scenario: Start the frontend
- **WHEN** dependencies are installed and `task dev` runs without a daemon
- **THEN** the shell is served on the configured strict port

#### Scenario: Validate and build
- **WHEN** `task check:frontend` runs
- **THEN** TypeScript and Svelte validation and the static production build pass or return a nonzero failure

### Requirement: Frontend interaction tests and verification integration

The repository SHALL provide `task test:frontend` for deterministic shell interaction tests without live provider or daemon calls. `task verify` SHALL include frontend checks and tests alongside existing contract, backend lint, test, and build checks. Documentation SHALL describe required frontend test tooling.

#### Scenario: Full repository verification
- **WHEN** `task verify` runs after installing documented prerequisites
- **THEN** it executes existing backend/contract checks plus frontend validation, production build, and interaction tests and fails if any fails

#### Scenario: Generated client stays contract-derived
- **WHEN** `task codegen` runs twice with the frontend scaffold present and the local contract unchanged
- **THEN** it generates the frontend schema only from `contracts/openapi.yaml` and the second run produces no generated-file changes
