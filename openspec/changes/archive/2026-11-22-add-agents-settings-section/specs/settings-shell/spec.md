# Spec Delta

## ADDED Requirements

### Requirement: Agents section lists the roster

Settings SHALL include an Agents section listing every configured agent
(name, description, model override when present, tool allowlist summary,
delegation grant) with an empty state pointing at the `~/.zbot/agents`
directory. Read-only v1: agents are file-defined and user-owned.

#### Scenario: Cards render
- **WHEN** Settings opens with two agents configured
- **THEN** both render as cards with their delegation metadata

#### Scenario: Empty state
- **WHEN** no agents are configured
- **THEN** the section shows guidance to create files under ~/.zbot/agents
