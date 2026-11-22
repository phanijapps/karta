# Spec Delta

## Purpose

Daemon-managed lifecycle for MCP tool servers: CRUD over `config/mcps/` manifests with immediate registry apply, plus a bounded connection probe that verifies a server configuration and lists its tools.

## ADDED Requirements

### Requirement: MCP server files and naming

An MCP server SHALL be exactly one manifest at `config/mcps/<name>.yaml` whose name is its file stem. Names of API-created and API-renamed servers SHALL match the managed-name rule (`^[a-z0-9][a-z0-9-]{0,63}$`, no Windows-reserved names); existing files with other stems or extensions remain loadable and manageable in place, with mutations addressing the loaded source file. A manifest SHALL declare at least one transport: a stdio server (`command` with optional `args` and `env`), a streamable HTTP server (`url`), or both — a server reachable both ways is one entry carrying two transports. Declaring neither SHALL be rejected. The `env` map is plain file content, not a secret store; no MCP field SHALL interact with the platform keyring.

#### Scenario: Stdio manifest round-trips

- **WHEN** an MCP server is created with a command, args, and an env map
- **THEN** the written manifest declares exactly that stdio configuration and loads back identically

#### Scenario: Legacy-named manifest is manageable in place

- **WHEN** a hand-authored `Old-Server.yml` exists with a broader stem or extension than the managed rule
- **THEN** edit and delete operate on that very file and a rename normalizes it to a managed-name `.yaml` manifest

#### Scenario: Dual-transport manifest round-trips

- **WHEN** an MCP server is created supplying both a command and a url
- **THEN** the written manifest declares both transports and loads back as one entry listed with both transports

#### Scenario: No transport rejected

- **WHEN** a create or edit supplies neither a command nor a url
- **THEN** the mutation is rejected with a validation error and no file changes

### Requirement: MCP CRUD applies immediately

Create, edit, and rename SHALL write the manifest atomically (never a partial file) using no-clobber installation — a target path occupied on disk fails as a duplicate whatever occupies it, including invalid manifests absent from the snapshot — and the registry snapshot SHALL reflect the completed mutation before the API response is sent; after any failed mutation the published registry state SHALL match what is on disk. Rename SHALL move the server to a new file stem; if removing the old file fails after the new one is published, the registry state matches disk (whatever still loads is listed) and the retry is idempotent. Delete SHALL remove the manifest and unregister the server. Creating a server whose name already exists SHALL fail without touching the existing manifest. Hand-edited files in the same directory SHALL keep working; external edits land through the registry's hot-reload exactly as before.

#### Scenario: Create is listable at once

- **WHEN** a server is created via the API
- **THEN** the create response succeeds and an immediate list includes the new server with no daemon restart

#### Scenario: Duplicate name is rejected

- **WHEN** a server is created with the name of an existing server
- **THEN** the mutation fails with a duplicate-name error and the existing manifest is unchanged

#### Scenario: Rename onto an occupied path conflicts

- **WHEN** a rename targets a stem whose path is occupied by an invalid manifest the snapshot never contained
- **THEN** the rename fails with a duplicate error and neither file changes

#### Scenario: Rename relocates the server

- **WHEN** a server is renamed
- **THEN** the server is listed under the new name, absent under the old one, and its configuration is preserved

#### Scenario: External edit still hot-reloads

- **WHEN** a user hand-edits a manifest created through the API
- **THEN** the registry reloads the file and the next list reflects the hand-edited configuration

### Requirement: MCP connection probe

A test operation SHALL verify a configured server by performing an MCP initialize handshake and tool listing for every declared transport (spawning the stdio command, connecting to the HTTP url — concurrently when both are declared), bounded by one deadline, and shutting each connection down afterwards. It SHALL return one result per declared transport, each carrying the discovered tool names and descriptions (capped, with truncation marked) or a typed failure: the command could not be spawned, the connection could not be established, the deadline expired, or the peer violated the protocol. A failing transport SHALL NOT affect the other transport's result. The probe SHALL NOT execute any tool, mutate registry state, or be affected by concurrent registry mutations; probe results SHALL NOT change the server's registered configuration.

#### Scenario: Healthy stdio server lists its tools

- **WHEN** the test runs against a stdio server that starts and completes the handshake
- **THEN** the server's tool names and descriptions are returned

#### Scenario: Dual-transport server yields per-transport results

- **WHEN** the test runs against a server declaring a working stdio command and an unreachable url
- **THEN** the stdio result lists tools and the http result is a typed connect failure in the same response

#### Scenario: Unreachable HTTP url is a typed failure

- **WHEN** the test runs against an HTTP url that refuses connections
- **THEN** the failure is a typed connect failure, not a generic error, and the registered manifest is unchanged

#### Scenario: Hanging server is bounded

- **WHEN** the tested server accepts the connection but never completes the handshake
- **THEN** the test fails with the deadline-expired failure within the bounded time

#### Scenario: No tool side effects

- **WHEN** the test completes against any server
- **THEN** no tool was executed and no registry or file state changed

#### Scenario: Oversized tool listing is truncated

- **WHEN** the tested server declares more tools than the listing cap
- **THEN** the result lists tools up to the cap and is marked truncated
