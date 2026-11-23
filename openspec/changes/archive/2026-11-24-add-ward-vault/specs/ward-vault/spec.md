# Spec Delta

## Purpose

Wards: self-describing directories of documented knowledge in a vault
outside ~/.zbot — vault config, typed layout schema, linter, archetype
templates, read-only listing.

## ADDED Requirements

### Requirement: The vault lives outside the daemon home

The ward vault root SHALL be a user-configured path (default
OS-aware `~/Documents/wards`, recorded operator decision in design.md —
the earlier `~/karta-wards` text was superseded during design) outside
`~/.zbot`: wards are user data and documented
knowledge, not daemon state. A missing vault directory SHALL be created
empty on first bootstrap; user files inside are never modified by the
vault machinery itself.

#### Scenario: First bootstrap creates the vault
- **WHEN** the daemon starts with no vault directory at the configured root
- **THEN** the directory is created and archetype templates are seeded; hand edits survive re-bootstrap

### Requirement: Wards carry a typed layout schema

Each ward MAY carry a `ward-conf.yaml` declaring its filesystem layout
as a declarative tree (node kinds file/directory, match patterns,
required/repeat flags, formats markdown/raw, $ref subtrees). The linter
SHALL validate a ward's actual tree against its conf and report typed
violations (missing required nodes, unexpected entries, format
mismatches). A ward without a conf is valid (generic freeform).

#### Scenario: Lint catches a missing required node
- **WHEN** a research-archetype ward is missing its canonical page
- **THEN** the lint report names the ward, the missing node id, and its expected match

#### Scenario: Freeform wards lint clean
- **WHEN** a ward has no ward-conf.yaml
- **THEN** it is listed with archetype generic and no lint violations

### Requirement: Wards are listable read-only

`GET /api/wards` SHALL list vault wards (name, archetype, lint status,
canonical title when present); `GET /api/wards/{name}` SHALL return the
parsed layout tree. No mutation endpoints in v1 — wards are created by
agents (later slice) or by hand.

#### Scenario: List the vault
- **WHEN** the vault holds two wards, one linting clean and one with violations
- **THEN** both list with their archetype and lint status

#### Scenario: Read one ward's layout
- **WHEN** a ward with a conf is read
- **THEN** the response carries its parsed tree (node ids, kinds, matches)
