# Spec Delta

## ADDED Requirements

### Requirement: Provider list in config.yaml

The config schema SHALL accept a `providers` list whose entries carry `name`, `type`, `base_url`, and an optional `api_key_env`. Loading SHALL validate every entry and reject the file with a startup error naming the file and the offending entry; an absent `providers` key SHALL load as an empty provider list.

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
