# Spec Delta

## MODIFIED Requirements

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
skills SHALL parse under the strict SKILL.md loader. The intent-analyst
system agent (`agents/intent-analyst.md`) is seeded once per home under
its OWN marker, so already-marked homes receive it on upgrade exactly
once and a user edit or deletion of it is final.

#### Scenario: User-owned examples survive re-bootstrap
- **WHEN** a user edits or deletes a seeded example agent or skill and
  bootstrap runs again
- **THEN** the edit is preserved byte-for-byte (edited files) and the
  deleted example is not recreated

#### Scenario: Seeded skills load cleanly
- **WHEN** the registry loads a freshly bootstrapped home
- **THEN** every seeded skill parses under the strict SKILL.md loader
  (name + description frontmatter) with zero load errors

#### Scenario: The analyst seeds once per home
- **WHEN** bootstrap runs on a home that already carries the roster marker
- **THEN** `agents/intent-analyst.md` is published once; a later edit or deletion survives every further bootstrap
