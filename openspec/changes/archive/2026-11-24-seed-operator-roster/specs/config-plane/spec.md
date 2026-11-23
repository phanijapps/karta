# Spec Delta

## Purpose

Codify bootstrap's example-content seeding: the agent example roster
(already shipped behavior) and the operator prompt-craft skills this
change adds — write-once, user-owned after first write.

## MODIFIED Requirements

### Requirement: First-run bootstrap creates structure, never content

When the resolved home does not exist, bootstrap SHALL create the tree:
`config.yaml` with documented defaults, the
`config/{prompts,agents,skills,hooks,mcps,providers}/` directories
(`config/skills/` carries the seeded example skills — see the added
requirement; the rest start empty), and `data/`; the ward vault is created separately by the wards package,
outside the home. Bootstrap seeds nothing except the example roster
(see "Bootstrap seeds the example roster"), never overwrites an
existing file, and is idempotent.

#### Scenario: First run on a missing home

- **WHEN** bootstrap runs against a nonexistent home
- **THEN** the full tree exists with a default `config.yaml`, the
  example agent roster, and the example skills

#### Scenario: Existing home is untouched

- **WHEN** bootstrap runs against a home that already has a
  user-authored `config.yaml`
- **THEN** that file's bytes are unchanged; the example roster MAY be
  seeded once (per the marker-gated "Bootstrap seeds the example roster"
  requirement), and no OTHER registry file is created

#### Scenario: Bootstrap is idempotent

- **WHEN** bootstrap runs twice in a row against the same home
- **THEN** the second run changes nothing

## ADDED Requirements

### Requirement: Bootstrap seeds the example roster

Bootstrap SHALL seed EXAMPLE content in exactly two places, write-once and crash-atomically (a file is published only complete):
the agent example roster (`agents/*.example.md`) and the example
prompt-craft skills (`config/skills/<name>/SKILL.md` plus bundled
`references/`). Seeding is ONCE PER HOME, gated by a marker file: fresh
homes AND pre-marker homes (upgrades) gain their missing examples; a
failed seeding run leaves no marker so the next bootstrap retries the
missing files. After the marker exists, bootstrap seeds nothing — user
edits survive byte-for-byte and a deleted example is NOT recreated.
Seeded skills SHALL parse under the strict SKILL.md loader.

#### Scenario: User-owned examples survive re-bootstrap

- **WHEN** a user edits or deletes a seeded example agent or skill and
  bootstrap runs again
- **THEN** the edit is preserved byte-for-byte (edited files) and the
  deleted example is not recreated

#### Scenario: Seeded skills load cleanly

- **WHEN** the registry loads a freshly bootstrapped home
- **THEN** every seeded skill parses under the strict SKILL.md loader
  (name + description frontmatter) with zero load errors
