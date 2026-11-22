# Spec Delta

## ADDED Requirements

### Requirement: Agents list endpoint

`GET /api/agents` SHALL return the registry's loaded agent definitions —
name, optional display name, optional description, optional model
override, tool allowlist, delegation grant (max_depth), and source file —
ordered by name. The endpoint is read-only.

#### Scenario: List agents
- **WHEN** `~/.zbot/agents` holds two frontmatter agents
- **THEN** the response lists both with their metadata, ordered by name

#### Scenario: Empty roster
- **WHEN** no agents are configured
- **THEN** the response is an empty list (200, not an error)
