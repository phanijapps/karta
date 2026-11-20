# Spec Delta

## Purpose

Governs the AGENTS.md constitution layer that every coding agent working in the z-Bot repository loads — the always-on architectural rules that keep agent-driven development aligned with the founding architecture brief.

## ADDED Requirements

### Requirement: Root constitution file

The repository root SHALL provide an `AGENTS.md` that states the four global rules from the architecture brief: local-first storage, contract-first API development, a channel-agnostic core, and mandatory eventbus emission.

#### Scenario: Agent session starts at repo root

- **WHEN** a coding agent opens a session with the repository root as working directory
- **THEN** the root `AGENTS.md` is loaded and states all four global rules

#### Scenario: Local-first storage rule

- **WHEN** a change plans where persistent state lives
- **THEN** the constitution directs all persistent state under `~/.zbot/` (data database and configuration tree)

#### Scenario: Contract-first API rule

- **WHEN** a change plans to add an HTTP endpoint without a contract update
- **THEN** the constitution's contract-first rule directs the plan to update `contracts/openapi.yaml` before server handlers

#### Scenario: Channel-agnostic core rule

- **WHEN** a proposed change adds a UI, CLI, Wails, or messaging-plugin import inside a core package
- **THEN** the constitution identifies it as a violation of the channel-agnostic core rule

#### Scenario: EventBus emission rule

- **WHEN** the constitution is consulted about agent steps, delegations, tool calls, hook decisions, or compaction events
- **THEN** it requires each to emit an event to the internal eventbus

### Requirement: Backend constitution file

The repository SHALL provide `backend/AGENTS.md` stating, in addition to the root rules: only `internal/engine/eino/` may import `github.com/cloudwego/eino`; compaction logic lives in `internal/compaction/` implementing the domain Compactor interface; and unit tests are table-driven with in-memory SQLite and fake LLM/engine stubs, never live APIs.

#### Scenario: Agent works inside backend/

- **WHEN** a coding agent's task touches files under `backend/`
- **THEN** `backend/AGENTS.md` is loaded and its scope rules apply in addition to the root constitution

#### Scenario: Engine import outside the adapter package

- **WHEN** a proposed backend change imports `github.com/cloudwego/eino` from any package other than `internal/engine/eino/`
- **THEN** the backend constitution identifies it as a violation of the engine isolation rule

#### Scenario: Compaction logic inside an engine adapter

- **WHEN** a proposed backend change hardcodes compaction logic inside an engine adapter instead of `internal/compaction/`
- **THEN** the backend constitution identifies it as a violation of the compaction placement rule

#### Scenario: Unit test hits a live API

- **WHEN** a proposed backend unit test calls a live LLM or engine API
- **THEN** the backend constitution's testing rule identifies it as a violation

### Requirement: Backend dependency graph rule

The backend constitution SHALL state the downward dependency graph that backend packages MUST follow: `domain` → `config`/`registry`/`eventbus` → `compaction`/`hooks`/`memory`/`tools`/`wards` → `engine/*` → `server` & `channels`.

#### Scenario: Placing a new backend package

- **WHEN** a new backend package is designed
- **THEN** the constitution's dependency graph determines which packages it may import

#### Scenario: Dependency direction violated

- **WHEN** a proposed change makes an earlier-layer package import a later-layer package (for example `domain` importing `engine`)
- **THEN** the backend constitution identifies it as a violation

### Requirement: Frontend constitution file

The repository SHALL provide `frontend/AGENTS.md` stating: Svelte 5 runes only (no legacy `$:` or `export let` syntax); FSD import direction `app` → `features` → `entities` → `shared` with no cross-feature imports; and API access only through the generated client.

#### Scenario: Agent works inside frontend/

- **WHEN** a coding agent's task touches files under `frontend/`
- **THEN** `frontend/AGENTS.md` is loaded and its scope rules apply in addition to the root constitution

#### Scenario: Frontend change uses legacy syntax

- **WHEN** a proposed frontend change introduces Svelte 4 reactive syntax (`$:` or `export let`)
- **THEN** the frontend constitution identifies it as a violation

#### Scenario: Cross-feature import

- **WHEN** a proposed frontend change imports one feature from another feature
- **THEN** the frontend constitution identifies it as a violation of the FSD import rules

#### Scenario: Hand-rolled API call

- **WHEN** a proposed frontend change calls the daemon API outside the generated client
- **THEN** the frontend constitution identifies it as a violation

### Requirement: Constitution is the authoritative rule record

The AGENTS.md files SHALL be the authoritative statement of the architecture brief's rules, and SHALL be updated in the same change that alters any rule they encode.

#### Scenario: Architecture rule changes

- **WHEN** a change modifies a rule the constitution encodes (for example, the dependency graph gains a package)
- **THEN** the corresponding AGENTS.md file is updated within that same change
