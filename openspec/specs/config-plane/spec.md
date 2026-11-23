# config-plane Specification

## Purpose

The control-plane foundation: home resolution with whole-root override, first-run bootstrap that creates structure but never content, and strict config.yaml loading that hard-fails on invalid input.

## Requirements

### Requirement: Home resolution with whole-root override

The config package SHALL resolve the z-Bot home in strict precedence: an explicit flag override (accepted as an API argument now, wired to the `zbotd` flag later), then the `ZBOT_HOME` environment variable, then the default `~/.zbot`. The override SHALL relocate the entire root (`config.yaml`, `config/**`, `data/`) — no partial overrides. The ward vault is NOT part of the home: it is user data resolved outside `~/.zbot` via `wards.vault_root` (see the ward-vault capability).

#### Scenario: Default resolution

- **WHEN** no flag override and no `ZBOT_HOME` are set
- **THEN** the resolved home is `~/.zbot`

#### Scenario: Environment override

- **WHEN** `ZBOT_HOME=/tmp/alt` is set and no flag override is given
- **THEN** the resolved home is `/tmp/alt`

#### Scenario: Flag beats environment

- **WHEN** a flag override `/tmp/flag-home` is given and `ZBOT_HOME=/tmp/alt` is set
- **THEN** the resolved home is `/tmp/flag-home`

### Requirement: First-run bootstrap creates structure, never content

When the resolved home does not exist, bootstrap SHALL create the tree:
`config.yaml` with documented defaults, the
`config/{prompts,agents,skills,hooks,mcps,providers}/` directories
(`config/skills/` carries the seeded example skills — see the
"Bootstrap seeds the example roster" requirement; the rest start
empty), and `data/`; the ward vault is created separately by the wards
package, outside the home. Bootstrap seeds nothing except the example
roster (see "Bootstrap seeds the example roster"), never overwrites an
existing file, and is idempotent.

#### Scenario: First run on a missing home

- **WHEN** bootstrap runs against a nonexistent home
- **THEN** the full tree exists with a default `config.yaml`, the
  example agent roster, and the example skills

#### Scenario: Existing home is untouched

- **WHEN** bootstrap runs against a home that already has a
  user-authored `config.yaml`
- **THEN** that file's bytes are unchanged; the example roster MAY be
  seeded once (per the marker-gated "Bootstrap seeds the example
  roster" requirement), and no OTHER registry file is created

#### Scenario: Bootstrap is idempotent

- **WHEN** bootstrap runs twice in a row against the same home
- **THEN** the second run changes nothing

### Requirement: Bootstrap seeds the example roster

Bootstrap SHALL seed EXAMPLE content in exactly two places, write-once
and crash-atomically (a file is published only complete): the agent
example roster (`agents/*.example.md`) and the example prompt-craft
skills (`config/skills/<name>/SKILL.md` plus bundled `references/`).
Seeding is ONCE PER HOME, gated by a marker file: fresh homes AND
pre-marker homes (upgrades) gain their missing examples; a failed
seeding run leaves no marker so the next bootstrap retries the missing
files. After the marker exists, bootstrap seeds nothing — user edits
survive byte-for-byte and a deleted example is NOT recreated. Seeded
skills SHALL parse under the strict SKILL.md loader.

#### Scenario: User-owned examples survive re-bootstrap

- **WHEN** a user edits or deletes a seeded example agent or skill and
  bootstrap runs again
- **THEN** the edit is preserved byte-for-byte (edited files) and the
  deleted example is not recreated

#### Scenario: Seeded skills load cleanly

- **WHEN** the registry loads a freshly bootstrapped home
- **THEN** every seeded skill parses under the strict SKILL.md loader
  (name + description frontmatter) with zero load errors

### Requirement: Strict config loading hard-fails on invalid input

Loading `config.yaml` SHALL strictly decode known fields and reject unknown keys. An absent file after bootstrap, invalid YAML, or unknown key SHALL produce a startup error naming the file, position, and offending key — the loader SHALL NOT fall back to defaults or regenerate the file.

#### Scenario: Unknown key rejected

- **WHEN** `config.yaml` contains a key outside the schema
- **THEN** loading fails with an error naming the unknown key and the file

#### Scenario: Invalid YAML rejected

- **WHEN** `config.yaml` is not valid YAML
- **THEN** loading fails with the parse error and file path

#### Scenario: Valid config loads

- **WHEN** `config.yaml` is valid with known keys
- **THEN** loading succeeds and returns the typed configuration

### Requirement: Directory tree self-heals

Loading with an existing home SHALL recreate any missing subdirectory of the control plane (`config/{prompts,agents,skills,hooks,mcps,providers}`, `data`) without touching any existing file.

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

### Requirement: Server address is loopback only

The daemon SHALL reject a configured `server.address` whose host is not a loopback address (`127.0.0.0/8`, `::1`, or the `localhost` name) with a startup error naming the file and the address. The daemon is local-first and unauthenticated; binding a non-loopback interface SHALL be a hard error, not a warning.

#### Scenario: Wildcard address rejected

- **WHEN** `config.yaml` sets `server.address` to `0.0.0.0:8787` or a non-loopback host
- **THEN** loading fails with an error naming the file and the address

#### Scenario: Loopback addresses accepted

- **WHEN** `server.address` is `127.0.0.1:8787`, `[::1]:8787`, or `localhost:8787`
- **THEN** loading succeeds
