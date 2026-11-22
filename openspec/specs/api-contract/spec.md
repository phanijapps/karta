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

The session list endpoint SHALL return bounded, explicitly truncated results from the store, and the session detail endpoint SHALL return identity and status.

#### Scenario: List is bounded and truncation is explicit

- **WHEN** the session list endpoint is called
- **THEN** the response bounds the number of entries and signals truncation when more exist

#### Scenario: Detail by identifier

- **WHEN** the session detail endpoint is called with an identifier
- **THEN** it returns 200 with the session's identity and status, or 404 with a typed error body for an unknown session

### Requirement: Timeline SSE stream

The session events endpoint SHALL stream that session's timeline events as SSE: replay of durable events first, then live events as they occur, ordered and gap-free, until the client disconnects. Events SHALL use the contract's TimelineEvent schema.

#### Scenario: Subscribe to a session timeline

- **WHEN** a client opens the session events endpoint
- **THEN** it first receives the session's durable history, then live events as the run progresses

#### Scenario: Event payload mirrors the domain

- **WHEN** the stream event schema is compared with the domain TimelineEvent kinds
- **THEN** every domain kind (agent step, delegation, tool call, hook decision, CompactionTriggered) is representable in the contract schema

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

The contract SHALL define `GET /api/chat/messages` returning transcript rows ordered by `seq` with bounded pages, a `next_cursor` that is the LAST DELIVERED sequence (a page cursor, never the database tail), and `truncated` indicating whether more replay rows remain. Clients page until `truncated` is false.

#### Scenario: Full replay pages to the end

- **WHEN** the endpoint is called with no cursor on a multi-page transcript
- **THEN** paging with each returned `next_cursor` yields every row exactly once, in order

#### Scenario: Incremental sync

- **WHEN** the endpoint is called with the last delivered cursor
- **THEN** it returns only newer rows and the new page cursor

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
