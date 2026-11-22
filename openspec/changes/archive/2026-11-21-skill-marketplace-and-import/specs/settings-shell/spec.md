# Spec Delta

## REMOVED Requirements

### Requirement: Live MCP and skill management

Replaced wholesale by the ADDED block below: the manual-create flow is
not the workflow (skills arrive by import/marketplace — user refinement,
evidence.md), so the "Create and edit a skill" scenario becomes "Edit a
skill" and the marketplace scenarios join the same requirement. Expressed
as REMOVED+ADDED (not MODIFIED) so the scenario rename is an explicit,
visible replacement rather than a silent drop.

## ADDED Requirements

### Requirement: Live MCP and skill management and marketplace import

MCP and Skills SHALL load their registries from the daemon and present them as live managed views. Each SHALL offer create, edit, delete with confirmation, per-file load-error display, and a daemon-connection state when unreachable — never fixture data or mock badges.

Skills SHALL present **Installed | Browse** views — the toggle is the only marketplace navigation (no duplicate browse button). Installed offers edit, delete, and detail, an **Import skill** action (the `.skill`/`.zip` file picker; manual form creation is removed — skills arrive via marketplace install or file import), and every skill SHALL show an origin badge (local, marketplace, imported). Browse SHALL offer a marketplace search box (daemon-proxied skills.sh index), results as cards (name, description, source repo, install counts) presented paginated with page navigation, a GitHub `owner/repo`/URL paste entry point, and an **Import file** picker accepting `.skill`/`.zip` archives. Every install path — marketplace result, GitHub paste, imported archive — SHALL pass through one mandatory preview dialog (rendered SKILL.md, complete file list, license, source) with the install action; index-unreachable, repository, and archive failures SHALL render typed with recoverable guidance. Third-party SKILL.md SHALL render INERTLY in the preview: no raw HTML, no automatic remote resource loading, dangerous URL schemes rejected, external links explicit — and the dialog labels the content untrusted. The preview dialog SHALL inherit the MCP popup's modality and async guarantees (focus entry/restoration, Tab containment, Escape closes only the preview, delayed responses bound to the initiating dialog while installs reconcile the list regardless).

MCP add and edit SHALL open a dialog with two modes chosen by a segmented control: **Form** and **Paste JSON**. The Form mode SHALL choose the transport with a segmented selector — Local process (stdio), Remote URL (http), or Both — showing only the fields the selection implies (command/args/env for Local and Both; URL for Remote and Both), never a field the selection makes irrelevant. The Paste JSON mode SHALL accept standard `mcpServers` JSON (and its accepted variants) and, on import, display one outcome row per server in the paste — created, skipped, or failed with its reason and warnings — refreshing the list behind the dialog; an edit dialog's Paste JSON mode SHALL prefill the server's current configuration as a standard block and save it as a full replace of that one server's transports through the daemon (its name unchanged — renames happen in Form mode; a multi-entry document in edit mode is rejected before anything changes). Switching modes SHALL keep each mode's unsaved draft while the dialog is open. Selecting a server SHALL open a detail popup (keyboard-modal: focus moves in and restores, Tab cycles inside, Escape closes only the popup) with its transports, an enable/disable switch, and the actions; the connection test SHALL display per-transport tool lists (with truncation noted) or typed failures with recoverable guidance. Form edits and renames SHALL preserve the server's current enablement — only an explicit switch action (or a full-replace body omitting `enabled`) changes it.

Skills SHALL load registered skills with name and description, show a skill's body on selection, and offer create (name, description, markdown body), edit, and delete whose confirmation names the directory and its bundled files.

#### Scenario: Inspect integrations

- **WHEN** a user selects a registered MCP server or skill
- **THEN** the relevant detail appears from daemon data (declared transports and their command or url for servers; body for skills)
- **AND** a hand-edited broken file is reported as a load error naming its path

#### Scenario: Search and install from the marketplace

- **WHEN** the user searches in Browse, previews a result, and installs it
- **THEN** the preview showed the SKILL.md and complete file list first, and the installed skill appears in the Installed list with a marketplace badge and no restart

#### Scenario: Untrusted markdown renders inertly

- **WHEN** a previewed SKILL.md embeds hostile HTML, a script URL, or
  a remote image
- **THEN** the preview renders it without executing, fetching, or
  navigating anywhere, labeled untrusted

#### Scenario: Import a skill file

- **WHEN** the user picks a `.skill` archive in Browse and commits the preview
- **THEN** the skill installs import-badged; a hostile archive (traversal, symlink, oversized, no SKILL.md) is rejected with its specific reason

#### Scenario: Marketplace failure is typed

- **WHEN** the index is unreachable or a repository holds no skills
- **THEN** the failure renders with recoverable guidance and no fabricated results

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
- **THEN** a popup shows its transports, file path, an enable/disable switch reflecting its state, and the Test/Edit/Delete actions; Escape closes the popup without closing Settings

#### Scenario: Enable and disable from the list

- **WHEN** the user flips a server's switch (row or popup)
- **THEN** the server is shown disabled (dimmed, marked) without leaving the list, the state persists in its manifest, and the server remains listed, toggleable, editable, deletable, and testable

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

#### Scenario: Edit a skill

- **WHEN** the user edits an installed skill's description or body
- **THEN** the list reflects the change immediately and the body round-trips through the daemon

#### Scenario: Search results paginate

- **WHEN** a search returns more results than one page holds
- **THEN** the results show one page at a time with working page navigation

#### Scenario: Delete names the bundle

- **WHEN** the user deletes a skill
- **THEN** a confirmation names the skill directory and its bundled files before anything is removed

#### Scenario: Daemon unreachable

- **WHEN** Settings opens with no daemon reachable
- **THEN** MCP and Skills show a daemon-connection state and no fixture servers or skills
