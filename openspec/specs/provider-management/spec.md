# provider-management Specification

## Purpose

Daemon-managed provider lifecycle: per-provider files under `~/.zbot/config/providers/`, merged with hand-authored `config.yaml` entries; OS-keyring credential storage; and the CRUD rules that make provider changes apply without a daemon restart.

## Requirements

### Requirement: Providers are declared in per-provider files or config.yaml

The daemon SHALL load providers from the `config.yaml` `providers:` list first, then from `~/.zbot/config/providers/<name>.yaml` (one provider per file). File entries carry `name`, `type`, `base_url`, and optionally `api_key_env` or the keyring marker `api_key: keyring`; config.yaml entries support `api_key_env` only. Both sources validate identically; duplicates within or across sources, and a combined count above the contract's list bound, are load errors naming the declarations.

#### Scenario: File-declared provider loads

- **WHEN** `config/providers/ollama.yaml` declares a provider and the daemon starts
- **THEN** it resolves by name alongside any config.yaml entries

#### Scenario: Duplicate name across sources rejected

- **WHEN** `config.yaml` and a file in `config/providers/` declare the same provider name
- **THEN** loading fails with an error naming the config.yaml entry and the file

#### Scenario: Keyring marker rejected in config.yaml

- **WHEN** a `config.yaml` provider entry carries `api_key: keyring`
- **THEN** loading fails naming the entry, since config.yaml supports environment references only

#### Scenario: Malformed file rejected with its path

- **WHEN** a file under `config/providers/` is invalid YAML, has unknown keys, or fails entry validation
- **THEN** loading fails naming that file and the offending detail

#### Scenario: Combined sources respect the list bound

- **WHEN** config.yaml and provider files together declare more providers than the contract's list bound
- **THEN** loading fails with an error naming the bound

### Requirement: Managed provider names are portable file stems

Daemon-created provider names SHALL match `^[a-z0-9][a-z0-9-]{0,63}$` and SHALL NOT be Windows-reserved device names (case-insensitive). A provider file's stem SHALL equal its declared name; a mismatch or a stem colliding under case-insensitive filesystems SHALL be a load error naming the file. Hand-authored `config.yaml` names keep the wider identifier pattern (they never become filenames).

#### Scenario: Create with an unportable name is rejected

- **WHEN** a create is attempted with an uppercase name, a reserved device name, or a name longer than 64 characters
- **THEN** it fails validation naming the managed-name rule

#### Scenario: File stem must match the declared name

- **WHEN** `config/providers/alpha.yaml` declares `name: beta`
- **THEN** loading fails naming the file and the mismatch

### Requirement: Provider changes apply without restart

After a provider is created, edited, or deleted through the daemon, its effect SHALL be observable through the provider endpoints immediately — no restart. External file changes are picked up via directory watching within a short bounded settle. Mutations and watcher reloads SHALL be serialized so a reload can never publish a stale set over a completed mutation, and a failed reload SHALL leave the last good set serving with the error logged.

#### Scenario: Create is immediately usable

- **WHEN** a provider is created through the API
- **THEN** a subsequent list includes it and model discovery against it succeeds without a restart

#### Scenario: Hand edit is picked up

- **WHEN** a file in `config/providers/` is edited while the daemon runs and the watcher settles
- **THEN** the provider list and discovery reflect the edited entry

#### Scenario: External breakage is surfaced

- **WHEN** a hand edit makes a file invalid or introduces a duplicate name
- **THEN** provider loading reports the error in the daemon log and the daemon keeps serving with the last good set

### Requirement: Mutations are compensating, not transactional

Mutations SHALL follow a fixed order — validate, prepare the secret (keyring write first), commit the file atomically, publish — with compensation: a file-commit failure after a keyring write deletes the just-written secret where possible (an accepted orphan is logged, unreadable, and overwritten on name reuse). A failed mutation SHALL leave the previous provider state operational and SHALL NOT write or remove any provider file.

#### Scenario: Create fails at the file commit

- **WHEN** the keyring write succeeds but the provider file cannot be written
- **THEN** the just-written secret is deleted where possible, no provider file exists, and the typed failure is returned

#### Scenario: Rename keeps the old provider on failure

- **WHEN** a rename fails at any step
- **THEN** the old provider still resolves by its old name and the new name was not published

### Requirement: CRUD semantics for file-declared providers

Create SHALL write a new `config/providers/<name>.yaml`, rejecting an existing name (either source) or a full list. Edit SHALL rewrite that file with rename support (old file removed, keyring re-keyed) and explicit credential semantics (absent retains; present replaces or clears). Delete SHALL remove the file and its keyring item best-effort. Config.yaml-declared providers SHALL be origin-protected with guidance to edit the file.

#### Scenario: Create persists a provider file

- **WHEN** a provider is created with a unique name
- **THEN** it is listed with a file origin and its file exists under `config/providers/`

#### Scenario: Duplicate create is a conflict

- **WHEN** a provider is created with a name that already exists in either source
- **THEN** the operation is refused with a typed conflict error and nothing is written

#### Scenario: Edit renames and re-keys

- **WHEN** a keyring-credential provider is renamed through the API
- **THEN** discovery under the new name sends the stored key and the old name no longer resolves

#### Scenario: Delete removes the file; secret cleanup is best-effort

- **WHEN** a keyring-credential provider is deleted and the keyring item cannot be removed
- **THEN** the provider file is gone, the name no longer resolves, and the orphan secret is overwritten if the name is later reused

#### Scenario: Config-origin provider is protected

- **WHEN** edit or delete targets a provider declared in `config.yaml`
- **THEN** the operation is refused with a typed origin error naming the file to edit instead

### Requirement: Secrets live in the OS keyring

The daemon SHALL store API-key values for API-created or API-edited providers in the platform keyring (Linux Secret Service, macOS Keychain, Windows Credential Manager), addressed by its product identity, the provider name, and a namespace of the resolved home root. Values appear in no file under `~/.zbot/`, no response, and no log. An unavailable keyring fails credential operations with a typed error — never a fallback — and calls carry a short deadline.

#### Scenario: Key is stored once and never echoed

- **WHEN** a provider is created with an API key
- **THEN** the key is retrievable from the keyring under the provider's home-scoped address and appears in no file, response, or log

#### Scenario: Keyring unavailable is a typed failure

- **WHEN** the platform keyring cannot be reached within the deadline
- **THEN** credential-carrying create/edit, keyring status checks, and keyring-credential discovery report a typed keyring failure with actionable guidance

#### Scenario: Relocated homes keep separate secrets

- **WHEN** two home roots each declare a provider with the same name and different keys
- **THEN** each home resolves its own key; writing or deleting in one leaves the other untouched

#### Scenario: Removing the credential clears the secret

- **WHEN** an edit replaces a keyring credential with `none`
- **THEN** the keyring item is deleted and subsequent requests are sent without credentials

### Requirement: Management behavior is verifiable without a keyring or live services

Automated tests SHALL run against in-memory or on-disk fakes for the secret store and in-process fake HTTP providers, making no live keyring or network calls, and SHALL include injected-failure and interleaving cases (file-commit failure after secret write, watcher reload racing a mutation, competing creates, delete during discovery). Keyring implementations and live discovery are exercised only by manual verification.

#### Scenario: Automated suite stays hermetic

- **WHEN** the automated tests run
- **THEN** secret-store scenarios use fakes and no request or keyring access leaves the process
