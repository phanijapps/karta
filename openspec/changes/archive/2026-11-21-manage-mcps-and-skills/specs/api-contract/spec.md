# Spec Delta

## ADDED Requirements

### Requirement: MCP endpoints

The contract SHALL define MCP list, create, edit, and delete endpoints. The list SHALL return every registered server with name, declared transports (stdio and/or streamable-http, each with its command or url), and per-file registry load errors (bounded, naming their paths); lists are complete within the per-kind capacity, with overflow surfaced as load errors rather than truncation. Create and edit bodies are full manifest replaces. Create SHALL return 201, 400 for an invalid name or manifest (no transport declared post-replace, malformed fields), 409 for a duplicate name or occupied target path, or 409 `mcp_limit` beyond capacity. Edit and delete SHALL address a server by name and return 200/204 on success, 404 for an unknown server, 409 for duplicates or capacity on rename, 400 for an invalid manifest, or 500 `io_error` for genuine write failures. No response field SHALL carry a credential value other than the env map the user authored in the manifest file.

#### Scenario: List includes load errors

- **WHEN** the MCP list endpoint is called while one hand-edited manifest is invalid
- **THEN** it returns 200 with the valid servers and a load error naming the invalid file's path

#### Scenario: Create with no transport is a 400

- **WHEN** a create supplies neither a command nor a url
- **THEN** it returns 400 with a typed validation error body and no manifest is written

#### Scenario: Dual-transport create is valid

- **WHEN** a create supplies both a command and a url
- **THEN** it returns 201 and the listed server carries both transports

#### Scenario: Rename onto an existing name is a 409

- **WHEN** an edit renames a server to an existing server's name
- **THEN** it returns 409 with a duplicate-name error body

#### Scenario: Rename onto an occupied path is a 409

- **WHEN** an edit renames a server to a stem whose file exists on disk but does not load
- **THEN** it returns 409 with a duplicate-name error body and neither file changes

#### Scenario: Edit clearing both transports is a 400

- **WHEN** an edit body replaces the manifest with neither a command nor a url
- **THEN** it returns 400 with a validation error body and the stored manifest is unchanged

#### Scenario: Create beyond capacity is a 409

- **WHEN** a create would push the server count past the per-kind capacity
- **THEN** it returns 409 with a capacity error body

#### Scenario: Write failure is a 500

- **WHEN** a validated create or edit fails during the filesystem commit
- **THEN** it returns 500 with an `io_error` body and the published list matches disk

### Requirement: MCP test endpoint

The contract SHALL define an MCP test endpoint addressed by server name that runs the connection probe and returns 200 with one result per declared transport — each carrying the discovered tool names and descriptions (capped, with truncation marked) or a typed failure reason (spawn failed, connect failed, deadline expired, protocol error) — or 404 for an unknown server. Failed probes SHALL NOT use 5xx statuses; the server configuration being broken is an expected, represented outcome.

#### Scenario: Probe failure is a typed 200 body

- **WHEN** the test endpoint is called for a server whose command does not exist
- **THEN** it returns 200 whose stdio result carries a spawn-failed failure

#### Scenario: Dual-transport probe returns both results

- **WHEN** the test endpoint is called for a server declaring both transports
- **THEN** it returns 200 with a stdio result and an http result, each independently succeeded or typed-failed

#### Scenario: Unknown server is a 404

- **WHEN** the test endpoint is called for an unregistered name
- **THEN** it returns 404 with a typed error body

### Requirement: Skill endpoints

The contract SHALL define skill list, detail, create, edit, and delete endpoints. The list SHALL return every registered skill with name, description, directory, and per-file registry load errors (bounded, naming their paths); lists are complete within the per-kind capacity. Detail SHALL return the name, description, and markdown body of one skill, 404 for an unknown skill. Create and edit bodies are full replaces of frontmatter and body (a changed name is the rename). Create SHALL return 201, 400 for invalid frontmatter (missing name or description, invalid name), 409 for a duplicate name or occupied target directory, or 409 `skill_limit` beyond capacity. Edit SHALL return 200, 400 for invalid frontmatter, 404 for an unknown skill, 409 for duplicates or capacity on rename, or 500 `io_error` for genuine write failures. Delete SHALL return 204, 404, or 500 `io_error`.

#### Scenario: Detail returns the body

- **WHEN** the skill detail endpoint is called for a registered skill
- **THEN** it returns 200 with the skill's name, description, and markdown body

#### Scenario: Create with empty description is a 400

- **WHEN** a skill create supplies no description
- **THEN** it returns 400 with a typed validation error body and no directory is written

#### Scenario: Create beyond capacity is a 409

- **WHEN** a skill create would push the skill count past the per-kind capacity
- **THEN** it returns 409 with a capacity error body

#### Scenario: Write failure is a 500

- **WHEN** a validated skill create, edit, or delete fails during the filesystem commit
- **THEN** it returns 500 with an `io_error` body and the published list matches disk
