# Spec Delta

## Purpose

Provides the on-demand development assets — workspace-local skills that teach specialized z-Bot workflows and named sub-agents for delegated development — so coding agents can execute and review architecture-specific work correctly.

## ADDED Requirements

### Requirement: Workspace-local development skills

The repository SHALL provide development skills at `.agents/skills/<name>/SKILL.md` for each of: `eino-engine-adapter`, `contract-first-api`, and `svelte5-fsd-frontend`. Each SKILL.md SHALL carry valid Agent Skills frontmatter with a `name` and a routing `description` stating what the skill covers and when to apply it. Skills SHALL live workspace-local only.

#### Scenario: Harness session starts in the repo

- **WHEN** an agent harness session starts with the repository as working directory
- **THEN** each development skill is discoverable by its name and description from within the workspace

#### Scenario: Task matches a skill domain

- **WHEN** a task involves implementing or modifying the Eino engine adapter, an API contract, or the Svelte frontend
- **THEN** the matching skill's instructions are loaded and followed for that task

#### Scenario: Skill placement stays workspace-local

- **WHEN** the change is applied and skills are created
- **THEN** all three skills exist under the repository's `.agents/skills/` and none are written to user-global `~/.agents/skills/` or `~/.pi/agent/skills/`

### Requirement: Skill content is grounded in the constitution and brief

Each skill SHALL encode the load-bearing rules and procedures for its domain as established by the architecture brief and the AGENTS.md constitution, and SHALL NOT instruct an agent to violate a constitution rule.

#### Scenario: Engine adapter skill coverage

- **WHEN** the `eino-engine-adapter` skill is consulted for engine work
- **THEN** it covers the isolation boundary, middleware adapter patterns, and the engine-swap procedure

#### Scenario: Contract-first API skill coverage

- **WHEN** the `contract-first-api` skill is consulted for an API change
- **THEN** it covers the contract → codegen → handler/SSE flow and forbids handler-first changes

#### Scenario: Frontend skill coverage

- **WHEN** the `svelte5-fsd-frontend` skill is consulted for frontend work
- **THEN** it covers Svelte 5 runes, FSD import rules, the generated API client, the Wails dev port configuration, and platform-adapter isolation

#### Scenario: Skill contradicts the constitution

- **WHEN** a skill's instructions are checked against the AGENTS.md files
- **THEN** no skill instructs an agent to violate a constitution rule

### Requirement: Named sub-agent roster

The repository SHALL provide sub-agent definitions at `.pi/agents/` for `zbot-scout`, `zbot-backend`, `zbot-frontend`, and `zbot-reviewer`. Each definition SHALL carry frontmatter with a unique `name`, a routing `description`, and tool permissions, plus a system prompt grounded in the constitution.

#### Scenario: Delegation by name

- **WHEN** a supervisor agent launches a sub-agent by one of the roster names
- **THEN** that sub-agent runs with its defined system prompt and tool permissions

#### Scenario: Worker agents inherit the constitution

- **WHEN** any roster agent runs on a task in this repository
- **THEN** its session loads the AGENTS.md constitution as project context

### Requirement: Reviewer agent is a read-only boundary checker

The `zbot-reviewer` agent SHALL be read-only and SHALL check diffs against the constitution's boundary rules: engine isolation, FSD import direction, dependency graph, contract-first API, and eventbus emission.

#### Scenario: Boundary-violating diff is reviewed

- **WHEN** `zbot-reviewer` reviews a diff that adds an eino import outside `internal/engine/eino/`
- **THEN** the review reports it as a constitution violation

#### Scenario: Reviewer cannot modify the repository

- **WHEN** `zbot-reviewer` is inspected or launched
- **THEN** its tool permissions allow reading only, with no file-writing or shell tools

### Requirement: Tooling evolves with the constitution

The skills and sub-agent definitions SHALL reference the AGENTS.md constitution as their authority, and SHALL be updated in the same change that alters a constitution rule they encode.

#### Scenario: Constitution rule changes

- **WHEN** a change modifies a constitution rule that a skill or sub-agent encodes
- **THEN** that skill or sub-agent definition is updated within the same change
