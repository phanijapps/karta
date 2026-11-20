# config-plane Specification

## Purpose

The control-plane foundation: home resolution with whole-root override, first-run bootstrap that creates structure but never content, and strict config.yaml loading that hard-fails on invalid input.

## Requirements

### Requirement: Home resolution with whole-root override

The config package SHALL resolve the z-Bot home in strict precedence: an explicit flag override (accepted as an API argument now, wired to the `zbotd` flag later), then the `ZBOT_HOME` environment variable, then the default `~/.zbot`. The override SHALL relocate the entire root (`config.yaml`, `config/**`, `data/`, `wards/`) — no partial overrides.

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

When the resolved home does not exist, bootstrap SHALL create the tree: `config.yaml` with documented defaults, empty `config/{prompts,agents,skills,hooks,mcps}/` directories, `data/`, and `wards/`. Bootstrap SHALL NOT seed any agent, prompt, skill, hook, or MCP content, SHALL NOT overwrite any existing file, and SHALL be idempotent.

#### Scenario: First run on a missing home

- **WHEN** bootstrap runs against a nonexistent home
- **THEN** the full tree exists with a default `config.yaml` and empty config subdirectories

#### Scenario: Existing home is untouched

- **WHEN** bootstrap runs against a home that already has a user-authored `config.yaml`
- **THEN** that file's bytes are unchanged and no registry file is created

#### Scenario: Bootstrap is idempotent

- **WHEN** bootstrap runs twice against the same home
- **THEN** the second run changes nothing

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

Loading with an existing home SHALL recreate any missing subdirectory of the control plane (`config/{prompts,agents,skills,hooks,mcps}`, `data`, `wards`) without touching any existing file.

#### Scenario: Partial home is repaired

- **WHEN** the home exists but `config/skills/` and `data/` are missing
- **THEN** loading recreates both directories and modifies no files
