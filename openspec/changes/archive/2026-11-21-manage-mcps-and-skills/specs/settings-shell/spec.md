# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: MCP and skill previews`
- TO: `### Requirement: Live MCP and skill management`

## MODIFIED Requirements

### Requirement: Settings navigation and draft lifecycle

Settings SHALL open a large desktop-style dialog with Providers, MCP, Skills, Logging, and Advanced navigation. It SHALL clearly identify live daemon views versus shell-only previews. Editable preview values in Logging and Advanced SHALL be drafts retained across section switches while open, discarded on close, and resettable to fixture values. No preview action SHALL claim configuration was saved or applied. Providers, MCP, and Skills SHALL be live daemon views whose data and mutations flow through the daemon; no fixture or mock data SHALL be presented in them.

#### Scenario: Navigate and discard a draft

- **WHEN** the user edits a preview setting in Logging or Advanced, changes sections, closes Settings, and reopens it
- **THEN** the draft survives the section switch but reopening displays original fixture values
- **AND** the dialog explains that no real configuration changed

#### Scenario: Managed sections are live

- **WHEN** the user opens Providers, MCP, or Skills
- **THEN** the presented data comes from the daemon and any mutation acts on daemon-managed configuration through the daemon

#### Scenario: Providers are not editable

- **WHEN** the user selects a provider declared in `config.yaml`
- **THEN** it presents read-only fields with a hint pointing at the file
- **AND** no edit or delete affordance is offered for it

### Requirement: Live MCP and skill management

MCP and Skills SHALL load their registries from the daemon and present them as live managed views. Each SHALL offer create, edit, delete with confirmation, per-file load-error display, and a daemon-connection state when unreachable — never fixture data or mock badges.

#### Scenario: Inspect integrations

- **WHEN** a user selects a registered MCP server or skill
- **THEN** the relevant detail appears from daemon data (declared transports and their command or url for servers; body for skills)
- **AND** a hand-edited broken file is reported as a load error naming its path

#### Scenario: Test connection lists tools

- **WHEN** the user runs the connection test for a reachable server
- **THEN** the discovered tool names are displayed

#### Scenario: Test connection reports a typed failure

- **WHEN** the tested server cannot be spawned, connected to, or completes its handshake in time
- **THEN** the matching typed failure is displayed with recoverable guidance
- **AND** no action claims the registered configuration changed

#### Scenario: Create and edit a skill

- **WHEN** the user creates a skill with a name, description, and markdown body, then edits the description
- **THEN** the list reflects both changes immediately and the body round-trips through the daemon

#### Scenario: Delete names the bundle

- **WHEN** the user deletes a skill
- **THEN** a confirmation names the skill directory and its bundled files before anything is removed

#### Scenario: Daemon unreachable

- **WHEN** Settings opens with no daemon reachable
- **THEN** MCP and Skills show a daemon-connection state and no fixture servers or skills
