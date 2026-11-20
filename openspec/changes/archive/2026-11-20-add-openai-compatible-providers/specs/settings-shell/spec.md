# Spec Delta

## MODIFIED Requirements

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
