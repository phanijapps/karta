# settings-shell Specification

## Purpose

Provide a coherent configuration navigation and preview experience without claiming that shell drafts configure the daemon or connect integrations.

## Requirements

### Requirement: Settings navigation and draft lifecycle

Settings SHALL open a large desktop-style dialog with Providers, MCP, Skills, Logging, and Advanced navigation. It SHALL clearly identify live daemon views versus shell-only previews. Editable preview values SHALL be drafts retained across section switches while open, discarded on close, and resettable to fixtures. No action SHALL claim configuration was saved or applied; provider management acts on daemon-managed providers only — config.yaml-declared providers offer no editing or saving.

#### Scenario: Navigate and discard a draft

- **WHEN** the user edits a preview setting in MCP, Skills, Logging, or Advanced, changes sections, closes Settings, and reopens it
- **THEN** the draft survives the section switch but reopening displays original fixture values
- **AND** the dialog explains that no real configuration changed

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

Advanced SHALL display clearly labeled engine, server address,
compaction-budget, and wards-root previews associated with the
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
