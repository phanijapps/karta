# Spec Delta

## ADDED Requirements

### Requirement: Server address is loopback only

The daemon SHALL reject a configured `server.address` whose host is not a loopback address (`127.0.0.0/8`, `::1`, or the `localhost` name) with a startup error naming the file and the address. The daemon is local-first and unauthenticated; binding a non-loopback interface SHALL be a hard error, not a warning.

#### Scenario: Wildcard address rejected

- **WHEN** `config.yaml` sets `server.address` to `0.0.0.0:8787` or a non-loopback host
- **THEN** loading fails with an error naming the file and the address

#### Scenario: Loopback addresses accepted

- **WHEN** `server.address` is `127.0.0.1:8787`, `[::1]:8787`, or `localhost:8787`
- **THEN** loading succeeds

## MODIFIED Requirements

### Requirement: First-run bootstrap creates structure, never content

When the resolved home does not exist, bootstrap SHALL create the tree: `config.yaml` with documented defaults, empty `config/{prompts,agents,skills,hooks,mcps,providers}/` directories, `data/`, and `wards/`. Bootstrap SHALL NOT seed any agent, prompt, skill, hook, MCP, or provider content, SHALL NOT overwrite any existing file, and SHALL be idempotent.

#### Scenario: First run on a missing home

- **WHEN** bootstrap runs against a nonexistent home
- **THEN** the full tree exists with a default `config.yaml` and empty config subdirectories

#### Scenario: Existing home is untouched

- **WHEN** bootstrap runs against a home that already has a user-authored `config.yaml`
- **THEN** that file's bytes are unchanged and no registry file is created

#### Scenario: Bootstrap is idempotent

- **WHEN** bootstrap runs twice against the same home
- **THEN** the second run changes nothing

### Requirement: Directory tree self-heals

Loading with an existing home SHALL recreate any missing subdirectory of the control plane (`config/{prompts,agents,skills,hooks,mcps,providers}`, `data`, `wards`) without touching any existing file.

#### Scenario: Partial home is repaired

- **WHEN** the home exists but `config/skills/` and `data/` are missing
- **THEN** loading recreates both directories and modifies no files

### Requirement: Provider list in config.yaml

The config schema SHALL accept a `providers` list whose entries carry `name`, `type`, `base_url`, and an optional `api_key_env` (environment references only; the keyring marker is not valid in this file). Loading validates every entry and rejects the file with a startup error naming the file and the offending entry; an absent key loads as an empty list. The file stays user-owned — provider management never writes it; managed files under `config/providers/` merge after this list.

#### Scenario: Absent providers key is valid

- **WHEN** `config.yaml` has no `providers` key
- **THEN** loading succeeds with an empty provider list

#### Scenario: Duplicate provider names rejected

- **WHEN** two entries declare the same `name`
- **THEN** loading fails with an error naming the file and the duplicated entry

#### Scenario: Unsupported provider type rejected

- **WHEN** an entry declares a `type` other than `openai-compatible`
- **THEN** loading fails with an error naming the file and the entry's type

#### Scenario: Invalid base URL rejected

- **WHEN** an entry's `base_url` is empty, not absolute, or not http(s)
- **THEN** loading fails with an error naming the file and the entry

#### Scenario: Base URL includes the API root

- **WHEN** an entry's `base_url` omits the provider's version prefix (for example `http://localhost:11434` instead of `http://localhost:11434/v1`)
- **THEN** loading succeeds and the configured value is used as-is; discovery appends only `/models`

#### Scenario: Managed sources coexist

- **WHEN** `config.yaml` declares providers and files exist under `config/providers/`
- **THEN** both load together per the provider-management capability and `config.yaml` is never rewritten

#### Scenario: Keyring marker rejected in config.yaml

- **WHEN** a `config.yaml` entry carries `api_key: keyring`
- **THEN** loading fails naming the entry
