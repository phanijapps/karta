# Spec Delta

## MODIFIED Requirements

### Requirement: Agent spec loading

The registry SHALL load declarative agent specs from `~/.zbot/agents/*.yaml` and from `*.md` files whose YAML frontmatter carries the spec, exposing name, role/system prompt (literal text or prompt-template reference), description, model overrides, tool allowlists, and max_depth. Bootstrap SHALL seed example agents there and SHALL one-time-migrate files from the legacy `config/agents` location when the new directory is absent — user-owned files are never overwritten.

#### Scenario: YAML agent spec loads

- **WHEN** `~/.zbot/agents/researcher.yaml` defines a valid spec
- **THEN** the registry exposes the agent named `researcher` with its prompt and tool settings

#### Scenario: Frontmatter agent spec loads

- **WHEN** `~/.zbot/agents/researcher.md` carries the same spec in YAML frontmatter
- **THEN** it is exposed identically to the YAML form

#### Scenario: Duplicate names reported

- **WHEN** two agent files declare the same agent name
- **THEN** the conflict is reported with both file paths and the registry keeps a deterministic winner

#### Scenario: Legacy location migrates once

- **WHEN** bootstrap runs with `config/agents` populated and `~/.zbot/agents` absent
- **THEN** the legacy files move to `~/.zbot/agents` and later edits there survive re-bootstrap
