# Spec Delta

## ADDED Requirements

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
