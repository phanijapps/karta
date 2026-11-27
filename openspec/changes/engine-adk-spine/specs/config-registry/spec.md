# config-registry

## ADDED Requirements

### Requirement: Agent reasoning effort
The agent spec (frontmatter and YAML forms) SHALL accept an optional
`reasoning_effort` field (`low` | `medium` | `high`; absent = the
provider's default). Unknown values SHALL fail the agent's load with a
named error (per-file error isolation).

#### Scenario: reasoning_effort loads from both forms
- **WHEN** an agent spec declares `reasoning_effort: low` (frontmatter or YAML)
- **THEN** the agent loads and the engine passes the effort to its model calls
#### Scenario: An unknown effort fails the agent's load
- **WHEN** an agent spec declares `reasoning_effort: turbo`
- **THEN** that agent fails to load with a field-naming error and other agents load unaffected
