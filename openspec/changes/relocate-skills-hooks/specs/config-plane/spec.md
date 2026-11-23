# Spec Delta

## MODIFIED Requirements

### Requirement: Home resolution with whole-root override

The config package SHALL resolve the z-Bot home in strict precedence:
flag override, then `ZBOT_HOME`, then `~/.zbot` (scenario wording
below). The override SHALL relocate the ENTIRE root — `config.yaml`,
`config/**`, `data/`, `agents/`, `skills/`, `hooks/` — with no partial
overrides. The ward vault is user data outside the home
(`wards.vault_root`, the ward-vault capability).

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

When the resolved home does not exist, bootstrap SHALL create the tree: `config.yaml` with documented defaults, the `config/{prompts,mcps,providers}/` directories, `agents/`, `skills/`, `hooks/`, and `data/`. The ward vault is created separately by the wards package, outside the home. Bootstrap seeds nothing except the example roster (agents, top-level skills, persona prompts) and SHALL run the legacy-location migration (the "Legacy skills and hooks relocate top-level" requirement) before seeding.

#### Scenario: First run on a missing home

- **WHEN** bootstrap runs against a nonexistent home
- **THEN** the full tree exists with a default `config.yaml`, the example roster, and empty classic `config/{skills,hooks}/` absent

#### Scenario: Existing home is untouched

- **WHEN** bootstrap runs against a home that already has a user-authored `config.yaml`
- **THEN** that file's bytes are unchanged; the example roster MAY be seeded once (per the marker-gated seeding requirement), and no OTHER registry file is created

#### Scenario: Bootstrap is idempotent

- **WHEN** bootstrap runs twice in a row against the same home
- **THEN** the second run changes nothing

#### Scenario: Classic install migrates once

- **WHEN** a home has skills under `config/skills/` and an empty top-level `skills/`
- **THEN** the entries move to `skills/` and a later bootstrap moves nothing

#### Scenario: Hand-moved home needs no migration

- **WHEN** the top-level `skills/` already holds the user's skills and `config/skills/` is empty
- **THEN** the migration moves nothing and the registry loads the top-level skills

### Requirement: Directory tree self-heals

Loading with an existing home SHALL recreate any missing subdirectory of the control plane (`config/{prompts,mcps,providers}`, `agents`, `skills`, `hooks`, `data`) without touching any existing file.

#### Scenario: Partial home is repaired

- **WHEN** the home exists but `skills/` and `data/` are missing
- **THEN** both directories are recreated and no file changes

## ADDED Requirements

### Requirement: Legacy skills and hooks relocate top-level

Bootstrap SHALL relocate legacy `config/skills/` and `config/hooks/`
entries into the top-level `skills/` and `hooks/` roots, one entry at
a time: an entry whose destination already exists is LEFT in place
(never overwritten, stays inert); otherwise it moves up. The
migration is naturally idempotent (empty or absent legacy dirs are a
no-op) and recognizes a hand-moved home as complete. Empty legacy
directories are left in place.

#### Scenario: Classic install migrates once

- **WHEN** a home has skills under `config/skills/` and an empty top-level `skills/`
- **THEN** the entries move to `skills/` and a later bootstrap moves nothing

#### Scenario: Hand-moved home needs no migration

- **WHEN** the top-level `skills/` already holds the user's skills and `config/skills/` is empty
- **THEN** the migration moves nothing and the registry loads the top-level skills

#### Scenario: Conflict never overwrites

- **WHEN** a legacy skill name also exists at the top-level root
- **THEN** the top-level copy stands and the legacy copy stays untouched

### Requirement: Skill frontmatter tolerates packaging keys

The strict SKILL.md loader SHALL accept — and ignore — the packaging keys `license`, `metadata`, and `allowed-tools` alongside `name` and `description`. Unknown keys beyond this sink set remain load errors.

#### Scenario: Raw operator skill loads

- **WHEN** a skill carries `license` and `metadata` frontmatter keys
- **THEN** it loads with its name and description and no load error

#### Scenario: Unknown key still rejected

- **WHEN** a skill carries a frontmatter key outside name/description/license/metadata/allowed-tools
- **THEN** loading reports a load error naming the key
