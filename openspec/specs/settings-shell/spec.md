# settings-shell Specification

## Purpose

Provide a coherent configuration navigation and preview experience without claiming that shell drafts configure the daemon or connect integrations.

## Requirements

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

### Requirement: Live provider list and connection test

Providers SHALL load the configured provider list from the daemon's provider endpoint and display each provider's name, connection type, base URL, credential status, and source (daemon-managed or `config.yaml`). Daemon-managed providers SHALL offer edit and delete (delete with confirmation); config.yaml providers SHALL be read-only with a hint to edit the file. The connection test SHALL request the provider's models through the daemon and display the discovered identifiers or the typed failure with recoverable guidance. When the daemon is unreachable, it SHALL show an explicit daemon-connection state, not fixture data.

#### Scenario: Inspect a configured provider

- **WHEN** the user opens Providers with the daemon running and providers configured
- **THEN** each provider's name, type, base URL, credential status, and source are displayed from daemon data
- **AND** no secret value is shown or requested

#### Scenario: Test connection lists models

- **WHEN** the user runs the connection test for a reachable provider
- **THEN** the discovered model identifiers are displayed

#### Scenario: Test connection reports a typed failure

- **WHEN** the tested provider is unreachable, rejects credentials, returns a malformed response, or its keyring credential cannot be resolved
- **THEN** the matching failure is displayed with guidance that does not suggest a secret was wrong when it was not

#### Scenario: Daemon unreachable

- **WHEN** Settings opens with no daemon reachable
- **THEN** the Providers section shows a daemon-connection state and no fixture providers

### Requirement: Logging controls are previews

Logging SHALL provide log-level and diagnostics previews with explicit unavailable live-tail, export, and file-location integration states. Changing a draft SHALL NOT change application or daemon logging or collect diagnostic data.

#### Scenario: Preview a logging level
- **WHEN** the user changes the logging-level draft
- **THEN** the dialog reflects only that draft and explains that logging integration is not implemented
- **AND** no logs are collected or exported

### Requirement: Advanced configuration previews

Advanced SHALL display clearly labeled engine, server address,
compaction-budget, and wards vault-root previews associated with the
existing configuration concepts. It SHALL distinguish existing
concepts from proposed UI-only settings and disable actions requiring
daemon mutation or filesystem access — **except the Distillation
card, which is a live control** (below).

#### Scenario: Preview advanced configuration
- **WHEN** the Advanced section is opened
- **THEN** the read-only previews render as before, visually distinct from the live Distillation card

### Requirement: Distillation card is live

Settings → Advanced SHALL include a live Distillation card backed by
the daemon: a provider/model picker fed by the model catalog and a
prompt editor (the daemon's current distillation prompt). Saving SHALL
persist through the daemon API and apply without restart; failures
SHALL surface inline without discarding unrelated drafts; when the
daemon is unreachable the card SHALL show the connection state instead
of stale fixtures.

#### Scenario: Model selection persists live
- **WHEN** the user picks a provider/model in the Distillation card and saves
- **THEN** the daemon's distillation settings update and the next distillation pass uses them

#### Scenario: Prompt round-trips
- **WHEN** the user edits and saves the prompt
- **THEN** reopening Settings shows the saved prompt verbatim (daemon-sourced, never fixtures)

#### Scenario: Unreachable daemon shows state, not fixtures
- **WHEN** the daemon is unreachable
- **THEN** the Distillation card shows the connection state and no fabricated values

### Requirement: Provider creation and editing

Providers SHALL offer an add flow (name, base URL, optional credential: API key for keyring storage or an environment-variable name) and an edit flow for daemon-managed providers (base URL, credential, rename). Submissions SHALL refresh the list on success; typed failures (duplicate, validation, capacity, keyring) SHALL render with actionable guidance without losing the draft. A submitted API key SHALL never be displayed again — only its status.

#### Scenario: Add a provider

- **WHEN** the user submits a unique name and base URL
- **THEN** the provider appears in the list immediately and is testable without a restart

#### Scenario: Duplicate name is recoverable

- **WHEN** the user submits a name that already exists
- **THEN** a conflict message is shown and the draft remains editable

#### Scenario: Keyring unavailable is recoverable

- **WHEN** a submission with an API key fails because the keyring is unavailable
- **THEN** the typed failure and guidance are shown and the draft remains editable

#### Scenario: Edit refreshes immediately

- **WHEN** the user saves an edit to a daemon-managed provider
- **THEN** the list reflects the new values without a restart

#### Scenario: Delete requires confirmation

- **WHEN** the user deletes a daemon-managed provider
- **THEN** a confirmation names the provider and its stored credential before removal, and the list refreshes on confirm

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

### Requirement: Intent analysis card is live

Settings → Advanced SHALL include a live Intent Analysis card backed by
the daemon: a provider/model picker fed by the model catalog (with each
model's context window when known), a read-only derivation line, and a
**Test analysis** action. Saving SHALL persist through the daemon API
and apply without restart; the card SHALL expose no token knobs; an
unreachable daemon SHALL show the connection state instead of stale
values.

#### Scenario: Model selection applies without restart
- **WHEN** the user picks a provider/model on the Intent Analysis card and saves
- **THEN** the daemon's intent-analysis settings update and the next root research run analyzes with that model

#### Scenario: The derivation is read-only and model-derived
- **WHEN** the card shows a model whose window was discovered, tabled, or unknown
- **THEN** the derivation line names the model, the window and its source, the full-window output allowance, the packet estimate, and "no trimming" — and offers no field to change any of them

#### Scenario: Test analysis reports the decision and its cost
- **WHEN** the user runs Test analysis
- **THEN** the card reports status, model, tokens in/out, duration, attempts, the contract summary, and whether the safety valve fired, or the failure reason

#### Scenario: Unreachable daemon shows state, not fixtures
- **WHEN** the daemon is unreachable
- **THEN** the card shows the connection state and no fabricated values
