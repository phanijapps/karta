# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: MCP and skill previews`
- TO: `### Requirement: Live MCP and skill management`

## MODIFIED Requirements

### Requirement: Live MCP and skill management

MCP and Skills SHALL load their registries from the daemon and present them as live managed views. Each SHALL offer create, edit, delete with confirmation, per-file load-error display, and a daemon-connection state when unreachable — never fixture data or mock badges.

MCP add and edit SHALL open a dialog with two modes chosen by a segmented control: **Form** and **Paste JSON**. The Form mode SHALL choose the transport with a segmented selector — Local process (stdio), Remote URL (http), or Both — showing only the fields the selection implies (command/args/env for Local and Both; URL for Remote and Both), never a field the selection makes irrelevant. The Paste JSON mode SHALL accept standard `mcpServers` JSON (and its accepted variants) and, on import, display one outcome row per server in the paste — created, skipped, or failed with its reason and warnings — refreshing the list behind the dialog; an edit dialog's Paste JSON mode SHALL prefill the server's current configuration as a standard block and save it as a full replace of that one server's transports through the daemon (its name unchanged — renames happen in Form mode; a multi-entry document in edit mode is rejected before anything changes). Switching modes SHALL keep each mode's unsaved draft while the dialog is open. The connection test SHALL display per-transport tool lists (with truncation noted) or typed failures with recoverable guidance.

Skills SHALL load registered skills with name and description, show a skill's body on selection, and offer create (name, description, markdown body), edit, and delete whose confirmation names the directory and its bundled files.

#### Scenario: Inspect integrations

- **WHEN** a user selects a registered MCP server or skill
- **THEN** the relevant detail appears from daemon data (declared transports and their command or url for servers; body for skills)
- **AND** a hand-edited broken file is reported as a load error naming its path

#### Scenario: Add via the form dialog

- **WHEN** the user adds a server with the transport selector on Local process
- **THEN** the dialog shows command, arguments, and environment fields only, and saving creates the server without any URL field having been shown

#### Scenario: Paste the standard block

- **WHEN** the user pastes the modelcontextprotocol.io quickstart `mcpServers` block and imports
- **THEN** each server in the paste gets an outcome row (created, skipped, or failed with its reason) and the created ones appear in the list immediately

#### Scenario: Edit as JSON round-trips

- **WHEN** the user opens edit, switches to Paste JSON, edits values (including newline-bearing arguments and multiline env), and saves
- **THEN** the dialog was prefilled with the server's current configuration as a standard block, the save replaced that server's manifest wholesale under its existing name, and a multi-entry paste is rejected before anything changes

#### Scenario: Selecting opens the detail popup

- **WHEN** the user clicks a server in the list
- **THEN** a popup shows its transports, file path, an enable/disable
  switch reflecting its state, and the Test/Edit/Delete actions; Escape
  closes the popup without closing Settings

#### Scenario: Enable and disable from the list

- **WHEN** the user flips a server's switch (row or popup)
- **THEN** the server is shown disabled (dimmed, marked) without
  leaving the list, the state persists in its manifest, and the server
  remains listed, toggleable, editable, deletable, and testable

#### Scenario: Form edits preserve enablement

- **WHEN** a disabled server is edited or renamed through the Form mode
- **THEN** it stays disabled — no silent re-enable

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
