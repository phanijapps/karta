# api-contract Specification

## Purpose

The initial daemon HTTP/SSE contract in `contracts/openapi.yaml` — the single source of truth every client and handler is written and generated against.

## Requirements

### Requirement: Contract exists with strict schema discipline

`contracts/openapi.yaml` SHALL be a valid OpenAPI 3.1 document in which every operation has an `operationId`, every object schema sets `additionalProperties: false` with explicit required fields, and identifiers are bounded (length and pattern).

#### Scenario: Contract validates

- **WHEN** the contract is checked with an OpenAPI 3.1 validator
- **THEN** it passes with no errors

#### Scenario: Every operation is callable by name

- **WHEN** the contract is inspected
- **THEN** each operation carries a unique `operationId` usable for client generation

### Requirement: Health endpoint

The contract SHALL define a liveness endpoint returning success when the daemon is up.

#### Scenario: Liveness probe

- **WHEN** the health endpoint is called on a running daemon
- **THEN** it returns 200 with a body identifying the daemon and version

### Requirement: Goal endpoints

The contract SHALL define endpoints to start a goal run for a session, cancel a session's active run, and resume a session with user input. Create SHALL accept an optional qualified `model` (`provider/model-id`) selecting the run's model, returning 201 with the session; refusing with typed guidance when no model is resolvable (request or agent). Cancel SHALL stop the active run. Resume SHALL answer 501 with a typed not-yet body until interactive goals land.

#### Scenario: Start a goal run

- **WHEN** the goal endpoint is called with a goal and a model
- **THEN** it returns 201 with the session identity and status for the started run

#### Scenario: No model resolvable

- **WHEN** the goal endpoint is called with no request model and no agent model
- **THEN** it returns a typed error with guidance to select a model

#### Scenario: Cancel and resume

- **WHEN** cancel or resume is called for a session
- **THEN** cancel stops the active run (200), and resume answers 501 typed not-yet

### Requirement: Session read endpoints

The session list endpoint SHALL return bounded, explicitly truncated results from the store, and the session detail endpoint SHALL return identity and status. The session STATE endpoint SHALL additionally carry the session's executions — id, agent, role (root / analyst / the delegated agent), status, prompt and completion tokens, started and finished times — so clients can show a run's cost without a new endpoint.

#### Scenario: List is bounded and truncation is explicit
- **WHEN** the session list endpoint is called
- **THEN** the response bounds the number of entries and signals truncation when more exist

#### Scenario: Detail by identifier
- **WHEN** the session detail endpoint is called with an identifier
- **THEN** it returns 200 with the session's identity and status, or 404 with a typed error body for an unknown session

#### Scenario: State carries the run's executions
- **WHEN** a research session's state is read after a run with two delegations
- **THEN** the response lists the root, the analyst and both child executions with their tokens and terminal status

### Requirement: Timeline SSE stream

The session events endpoint SHALL stream that session's timeline events as SSE: replay of durable events first, then live events as they occur, ordered and gap-free, until the client disconnects. Events SHALL use the contract's TimelineEvent schema, and every frame — replayed or live — SHALL carry the acting `agent` and its `execution_id`.

#### Scenario: Subscribe to a session timeline
- **WHEN** a client opens the session events endpoint
- **THEN** it first receives the session's durable history, then live events as the run progresses

#### Scenario: Event payload mirrors the domain
- **WHEN** the stream event schema is compared with the domain TimelineEvent kinds
- **THEN** every domain kind (agent step, delegation, tool call, hook decision, CompactionTriggered, delegation started/completed) is representable in the contract schema

#### Scenario: Frames are attributed
- **WHEN** a child's tool call is streamed and later replayed
- **THEN** both frames carry the child's agent name and execution id

### Requirement: Additive contract evolution

Within a contract version, changes SHALL be additive (new optional fields, new operations); removing or renaming fields or changing semantics SHALL bump the major version.

#### Scenario: Additive change keeps the version

- **WHEN** a new endpoint or optional field is added
- **THEN** the minor version increments and existing clients remain valid

#### Scenario: Breaking change bumps major

- **WHEN** a field is removed or renamed
- **THEN** the major version is bumped

### Requirement: Provider list endpoint

The contract SHALL define a provider list endpoint returning the configured providers, each with name, type, base URL, credential environment-variable name when set, and a flag for whether that credential is currently configured. No response field SHALL carry a secret value.

#### Scenario: List providers without secrets

- **WHEN** the provider list endpoint is called on a running daemon
- **THEN** it returns 200 with the configured providers and no field carries a credential value

### Requirement: Provider models endpoint

The contract SHALL define a provider models endpoint that proxies OpenAI-compatible model discovery for a named provider. It SHALL return 200 with the discovered model identifiers, 404 with a typed error body for an unknown provider, or 502 with a typed error body distinguishing provider unreachable, provider authentication failure, and malformed provider response.

#### Scenario: Discover models through the daemon

- **WHEN** the provider models endpoint is called for a configured, reachable provider
- **THEN** it returns 200 with the provider's discovered model identifiers

#### Scenario: Unknown provider

- **WHEN** the provider models endpoint is called with a name that is not configured
- **THEN** it returns 404 with a typed error body

#### Scenario: Provider failure is typed

- **WHEN** the provider is unreachable, rejects credentials, or returns a malformed model list
- **THEN** the endpoint returns 502 with a typed error body identifying which failure occurred

### Requirement: Distillation settings endpoint

The contract SHALL define `GET /api/distillation` returning the
current distillation settings (provider name, model id, prompt text)
and `PUT /api/distillation` accepting a partial update of the same
shape, applied by the daemon without restart (daemon-managed settings
file; provider and model validated against the configured providers).
GET SHALL reflect daemon truth; PUT of an unknown provider or empty
model SHALL fail with a typed error.

#### Scenario: Round-trip settings
- **WHEN** `PUT /api/distillation` sets provider and model, then `GET` is called
- **THEN** the response carries the saved values and the next distillation pass uses them

#### Scenario: Unknown provider rejected
- **WHEN** `PUT /api/distillation` names a provider that is not configured
- **THEN** the daemon responds with a typed error naming the provider

### Requirement: Chat send endpoint

The contract SHALL define `POST /api/chat/messages` accepting the message text and the selected model (`provider/model-id`), returning every row the exchange appended in order (user row, any tool rows, final assistant row) with the exchange's end cursor. Typed failures SHALL return the persisted user row and its cursor. It SHALL return a typed error when no model is selected (400) and typed provider failures (502 with reason) or keyring-unavailable outcomes matching the provider endpoints.

#### Scenario: Send returns the full exchange

- **WHEN** the send endpoint is called with text and a selected model
- **THEN** it returns the appended user message, any tool rows, and the assistant reply in order

#### Scenario: Send without a model

- **WHEN** the send endpoint is called without a model selection
- **THEN** it returns 400 with guidance to pick a model

#### Scenario: Provider failure returns the persisted row

- **WHEN** the completion fails upstream
- **THEN** it returns the typed failure together with the persisted user message and cursor, and the transcript keeps any committed intermediate rows

### Requirement: Chat transcript sync endpoint

The contract SHALL define `GET /api/chat/messages` returning transcript rows ordered by `seq` with bounded pages, a `next_cursor` that is the LAST DELIVERED sequence (a page cursor, never the database tail), and `truncated` indicating whether more replay rows remain. Clients page until `truncated` is false. Rows SHALL carry the acting `agent` ("root" or the sub-agent's name) and their `execution_id` when known.

#### Scenario: Full replay pages to the end
- **WHEN** the endpoint is called with no cursor on a multi-page transcript
- **THEN** paging with each returned `next_cursor` yields every row exactly once, in order

#### Scenario: Incremental sync
- **WHEN** the endpoint is called with the last delivered cursor
- **THEN** it returns only newer rows and the new page cursor

#### Scenario: Rows name their author
- **WHEN** a delegated child's report row is synced
- **THEN** it carries the child's agent name and execution id; the root's rows carry "root"

### Requirement: Provider create endpoint

The contract SHALL define provider create accepting name, type, base URL, and an optional credential object (`none`, `env` with a variable name, or `keyring` with a one-shot API-key value). It SHALL return 201 with the provider and its credential status — never a credential value — 409 for an existing name or a full provider list, 400 for validation failures including unportable managed names, and 503 with a typed error body when the keyring is unavailable.

#### Scenario: Create a provider with a keyring credential

- **WHEN** the create endpoint is called with a name, base URL, and keyring credential
- **THEN** it returns 201 with the provider, the key is stored in the platform keyring, and no response field carries the key

#### Scenario: Duplicate create conflicts

- **WHEN** the create endpoint is called with an existing provider name
- **THEN** it returns 409 with a typed error body

#### Scenario: Create at list capacity conflicts

- **WHEN** the create endpoint is called while the combined sources already fill the provider list bound
- **THEN** it returns 409 with a typed capacity error

#### Scenario: Keyring unavailable

- **WHEN** a credential-carrying create is attempted while the keyring is unreachable
- **THEN** it returns 503 with a typed keyring error and no provider file is written

### Requirement: Provider edit and delete endpoints

Edit SHALL accept base URL, optional rename, and an optional credential object (absent retains; present replaces or clears), returning 200; 404 unknown; 409 conflict or origin-protected; 400 validation; 503 when a submitted key cannot be stored. Delete SHALL return 204, 404, or the same origin error. Listing entries SHALL carry `origin` (`file`|`config`) and `credential_status` (`unset`|`ready`|`missing`|`unavailable`).

#### Scenario: Edit a daemon-managed provider

- **WHEN** the edit endpoint is called for a file-declared provider, including a rename
- **THEN** it returns 200 with the updated provider, immediately visible in listings

#### Scenario: Edit retains credential when the field is absent

- **WHEN** an edit changes only the base URL
- **THEN** the provider's credential reference and stored secret are unchanged

#### Scenario: Edit clears a credential

- **WHEN** an edit submits the `none` credential object
- **THEN** the keyring item is deleted and subsequent requests carry no credential

#### Scenario: Config-origin provider is protected

- **WHEN** edit or delete is called for a provider declared in `config.yaml`
- **THEN** it returns 409 with a typed origin error naming the file to edit instead

#### Scenario: Delete a provider

- **WHEN** the delete endpoint is called for a file-declared provider
- **THEN** it returns 204 and the provider no longer appears in listings or resolves by name

### Requirement: Discovery failure reason includes credential unavailability

The provider models endpoint's typed 502 body SHALL distinguish a keyring credential that cannot be resolved (locked, stalled, or unreachable secret service) from provider-side unreachable, unauthorized, and malformed failures.

#### Scenario: Keyring stall is typed

- **WHEN** model discovery is requested for a keyring-credential provider whose secret service does not answer within the deadline
- **THEN** the endpoint returns a typed credential-unavailable failure naming the keyring

### Requirement: Model catalog endpoint

The contract SHALL define `GET /api/models` returning the daemon's catalog: a flat `models` array of `{ provider, id }` entries plus per-provider `refreshed_at` timestamps, served from memory (200, possibly empty). The endpoint SHALL NOT trigger provider requests.

#### Scenario: Read the catalog

- **WHEN** `GET /api/models` is called
- **THEN** it returns 200 with the catalogued models and refresh timestamps, without contacting any provider

### Requirement: Provider refresh endpoint

The contract SHALL define `POST /api/providers/{name}/models/refresh` performing a live model fetch for one provider, updating the catalog, and returning the fetched list. It SHALL return 404 for an unknown provider and 502 with the typed provider-failure reason (unreachable, unauthorized, malformed, credential unavailable) without changing the catalog.

#### Scenario: Refresh a provider

- **WHEN** the refresh endpoint is called for a reachable provider
- **THEN** it returns 200 with the fetched models and the catalog is updated

#### Scenario: Refresh an unknown provider

- **WHEN** the refresh endpoint is called with an unconfigured name
- **THEN** it returns 404 with a typed error body

#### Scenario: Refresh failure is typed and non-destructive

- **WHEN** the provider fails during refresh
- **THEN** it returns 502 with the failure reason and the catalog keeps the previous entry

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

### Requirement: MCP import endpoint

The contract SHALL define an MCP import endpoint accepting the pasted document as RAW TEXT plus an optional `name` (single-server pastes) and an optional `replaceName` (edit-as-JSON: exactly one entry, replacing that server's transports wholesale — its name unchanged; a multi-entry document with `replaceName`, or an entry that would skip or fail, rejects or reports without touching the target). It SHALL return 200 with one typed outcome per entry in paste order — created (stored name, warnings), skipped (duplicate, disabled, other-OS), or failed (shape, field, name, capacity, `io_error`) — each naming its original key; individual entry failures never fail the request. It SHALL return 400 with a typed error for every document-level rejection: malformed JSON, duplicate keys, trailing content, BOM or non-UTF-8 text, JSON non-object, unclassifiable shape, no recognizable servers, single-server paste without `name`, payload over 256 KiB, or more than 32 entries. The request body's document field SHALL be a string; the daemon is the only parser.

#### Scenario: Mixed paste returns per-entry outcomes

- **WHEN** the import endpoint receives a paste with one valid entry, one duplicate, and one malformed entry
- **THEN** it returns 200 with three outcome rows — created, skipped-duplicate, failed — in paste order, each naming its original key and reason

#### Scenario: Document-level rejection is a 400 creating nothing

- **WHEN** the paste has trailing content, a duplicated key, or 33 entries
- **THEN** it returns 400 with a typed error body and no manifest is written

#### Scenario: Edit-as-JSON replaces one server

- **WHEN** the endpoint receives a single-entry document with `replaceName` naming an existing server
- **THEN** that server's manifest is replaced wholesale (name unchanged) and the outcome reports it created-at-replace; a second entry in the document rejects the request without touching the target

### Requirement: MCP enablement field

MCP responses SHALL carry `enabled` (boolean). MCP create and edit
bodies MAY carry `enabled` (absent means enabled); edit is full-replace,
so an absent field re-enables the server.

#### Scenario: Toggle round-trips

- **WHEN** a server is edited with `enabled: false` and then listed
- **THEN** the response reports `enabled: false` and the manifest file
  persists the state

### Requirement: Skill marketplace endpoints

The contract SHALL define daemon-proxied marketplace endpoints:
search (query → results with name, description, source, install
count; typed index-unreachable failure), preview (a canonical GitHub
`owner/repo` identity → the parsed SKILL.md, complete file list with
sizes, license, the resolved immutable commit, AND an opaque preview
capability binding repo, commit, skill paths, and per-file digests),
and install (the capability plus the chosen skill subset → per-skill
created/skipped/failed outcomes, MCP-import semantics). Clients never
dial the index or GitHub directly. The failure matrix SHALL be typed
end to end: index-unreachable, upstream-unreachable, upstream
rate-limited (with retry guidance), repo-not-found, no-skills-found,
archive violations, invalid-skill, changed-upstream, duplicate,
capacity, io_error.

#### Scenario: Search through the daemon

- **WHEN** the search endpoint is called with a query and the index is
  reachable
- **THEN** it returns 200 with the index's results and no client-side
  fetch of the index occurs

#### Scenario: Preview binds and install consumes

- **WHEN** the preview endpoint is called for a public repository and
  the install endpoint follows with the capability and a skill subset
- **THEN** preview returns the content, file list, commit, and
  capability, and install returns one outcome per chosen skill

### Requirement: Skill archive import endpoints

The contract SHALL define a two-step file import: a preview request
carrying the `.skill`/`.zip` archive as multipart/form-data with
EXACTLY one file part named `archive` (no other parts, body ≤10 MiB
enforced before parsing) — the only endpoint exempt from the
JSON-mutation rule, protected instead by trusted-Host plus Origin
enforcement; and a commit request carrying the opaque staging id.
Preview rejections (no SKILL.md, traversal entries, symlinks, cap
violations, corrupt archive, extra parts, reserved provenance path)
SHALL be typed 400s naming the violation; commit of an unknown,
expired, or already-consumed staging id SHALL 404; exceeding the
concurrent staging bound SHALL be a typed busy failure.

#### Scenario: Preview then commit

- **WHEN** a valid archive is previewed and its staging id committed
- **THEN** the skill installs and both responses carry the skill's
  parsed content and origin

#### Scenario: Staging expires or replays

- **WHEN** a commit arrives for a staging id past its TTL or already
  consumed
- **THEN** it returns 404 and nothing installs

### Requirement: Skill provenance fields

Skill list and detail responses SHALL carry an `origin`
(`local` | `marketplace` | `import`); marketplace and imported skills
SHALL expose their source repository and ref when recorded.

#### Scenario: Origins in listings

- **WHEN** skills of each origin are installed
- **THEN** the list distinguishes them and the marketplace entry names
  its source repository

### Requirement: Chat streaming send endpoint

`POST /api/chat/send` SHALL accept the same body as the synchronous send and
return `text/event-stream`: zero or more `chat_delta` events (data carries
`kind` — `reasoning` or `content` — and a `text` fragment) and exactly one
terminal `chat_exchange` event whose payload matches the synchronous
response schema (rows, end cursor, optional typed error — mid-stream
provider failures surface in the terminal exchange's error field, the same
single truth as the synchronous path) before the stream closes. The event
schemas SHALL be strict (additionalProperties: false), the endpoint
additive, and the generated clients typed against them.

#### Scenario: Happy stream shape
- **WHEN** a streamed send succeeds
- **THEN** the stream contains only chat_delta events followed by exactly one chat_exchange, then closes

#### Scenario: Bad request fails before streaming
- **WHEN** the body is empty text or an invalid model reference
- **THEN** the endpoint responds with the same typed 4xx JSON error as the synchronous endpoint, without opening a stream

#### Scenario: Typed failure mid-stream
- **WHEN** the provider fails after deltas began
- **THEN** the terminal exchange carries the typed error, the user row (and any committed tool rows) persist, no final-answer row exists, and the stream closes

### Requirement: Agents list endpoint

`GET /api/agents` SHALL return the registry's loaded agent definitions —
name, optional display name, optional description, optional model
override, tool allowlist, delegation grant (max_depth), and source file —
ordered by name. The endpoint is read-only.

#### Scenario: List agents
- **WHEN** `~/.zbot/agents` holds two frontmatter agents
- **THEN** the response lists both with their metadata, ordered by name

#### Scenario: Empty roster
- **WHEN** no agents are configured
- **THEN** the response is an empty list (200, not an error)
