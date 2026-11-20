# Spec Delta

## Purpose

Provide a coherent configuration navigation and preview experience without claiming that shell drafts configure the daemon or connect integrations.

## ADDED Requirements

### Requirement: Settings navigation and draft lifecycle

Settings SHALL open a large desktop-style dialog with Providers, MCP, Skills, Logging, and Advanced navigation. It SHALL clearly identify shell-only operation and non-persistence. Editable values SHALL be drafts retained across section switches while open, discarded on close, and resettable to fixtures. No action SHALL claim configuration was saved or applied.

#### Scenario: Navigate and discard a draft
- **WHEN** the user edits a setting, changes sections, closes Settings, and reopens it
- **THEN** the draft survives the section switch but reopening displays original fixture values
- **AND** the dialog explains that no real configuration changed

### Requirement: Config-shaped provider preview

Providers SHALL show a fixture list and editable preview fields for a provider name, connection type, base URL, model identifiers, and credential environment-variable reference. It SHALL label the conceptual source as `~/.zbot/config.yaml` and identify unsupported schema fields as proposed preview fields, not existing daemon configuration.

#### Scenario: Inspect a provider
- **WHEN** the user selects a fixture provider and edits its preview
- **THEN** the fields show an in-memory draft and a visible warning that the shell neither reads nor writes the configuration file
- **AND** no secret value entry, credential lookup, connection test, or provider discovery occurs

### Requirement: MCP and skill previews

MCP SHALL show fixture servers with transport and command or URL previews, sourced conceptually from `~/.zbot/config/mcps/`. Skills SHALL show fixture names, descriptions, and `SKILL.md` path previews under `~/.zbot/config/skills/`. Both sections SHALL label data as illustrative and actions requiring real integration as unavailable.

#### Scenario: Inspect integrations
- **WHEN** a user selects a mock MCP server or skill
- **THEN** the relevant detail preview appears with mock status
- **AND** the shell does not spawn a process, connect to a server, install a skill, or read local files

### Requirement: Logging controls are previews

Logging SHALL provide log-level and diagnostics previews with explicit unavailable live-tail, export, and file-location integration states. Changing a draft SHALL NOT change application or daemon logging or collect diagnostic data.

#### Scenario: Preview a logging level
- **WHEN** the user changes the logging-level draft
- **THEN** the dialog reflects only that draft and explains that logging integration is not implemented
- **AND** no logs are collected or exported

### Requirement: Advanced configuration previews

Advanced SHALL display clearly labeled engine, server address, compaction-budget, and wards-root previews associated with the existing configuration concepts. It SHALL distinguish existing concepts from proposed UI-only settings and disable actions requiring daemon mutation or filesystem access.

#### Scenario: Preview advanced configuration
- **WHEN** the user opens Advanced and edits a supported preview field
- **THEN** only the dialog draft changes and the source-path hint remains explanatory text
- **AND** no engine switch, restart, filesystem opening, or real reset occurs
