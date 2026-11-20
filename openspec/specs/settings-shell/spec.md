# settings-shell Specification

## Purpose

Provide a coherent configuration navigation and preview experience without claiming that shell drafts configure the daemon or connect integrations.

## Requirements

### Requirement: Settings navigation and draft lifecycle

Settings SHALL open a large desktop-style dialog with Providers, MCP, Skills, Logging, and Advanced navigation. It SHALL clearly identify which sections are live daemon views and which are shell-only previews. Editable preview values SHALL be drafts retained across section switches while open, discarded on close, and resettable to fixture values. No action SHALL claim configuration was saved or applied; Providers SHALL offer no editing or saving of provider configuration.

#### Scenario: Navigate and discard a draft

- **WHEN** the user edits a preview setting in MCP, Skills, Logging, or Advanced, changes sections, closes Settings, and reopens it
- **THEN** the draft survives the section switch but reopening displays original fixture values
- **AND** the dialog explains that no real configuration changed

#### Scenario: Providers are not editable

- **WHEN** the user views the Providers section
- **THEN** it presents the daemon-configured providers without draft editing of provider fields
- **AND** no action claims provider configuration was saved or applied
### Requirement: Config-shaped provider preview

Providers SHALL load the configured provider list from the daemon's provider endpoint and display each provider's name, connection type, base URL, and credential environment-variable name with its configured status. It SHALL offer a connection test that requests the provider's models through the daemon and displays the discovered model identifiers, or the typed failure (unreachable, authentication, malformed) with user-recoverable guidance. It SHALL label `~/.zbot/config.yaml` as the source of provider configuration and communicate that adding or changing providers means editing that file (the daemon does not write it). When the daemon is unreachable, it SHALL show an explicit daemon-connection state, not fixture data.

#### Scenario: Inspect a configured provider

- **WHEN** the user opens Providers with the daemon running and `config.yaml` declaring a provider
- **THEN** the provider's name, type, base URL, and credential status are displayed from daemon data
- **AND** no secret value is shown or requested

#### Scenario: Test connection lists models

- **WHEN** the user runs the connection test for a reachable provider
- **THEN** the discovered model identifiers are displayed

#### Scenario: Test connection reports a typed failure

- **WHEN** the tested provider is unreachable, rejects credentials, or returns a malformed response
- **THEN** the matching failure is displayed with guidance that does not suggest a secret was wrong when it was not

#### Scenario: Daemon unreachable

- **WHEN** Settings opens with no daemon reachable
- **THEN** the Providers section shows a daemon-connection state and no fixture providers
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
