# Spec Delta

## MODIFIED Requirements

### Requirement: Marketplace install

Installing SHALL land each selected skill atomically under
`skills/<slug>/` (the top-level `~/.zbot/skills/` root) by the
registry's rules (slug from the skill name, capacity, duplicate
refusal leaving the existing skill untouched) through a
validate-stage-rename primitive: every file path and mode
(0o644/0o755 only) validated, the SKILL.md validated with the
LOADER's exact rules (accepted frontmatter: `name`, `description`,
and the ignored sink keys `path`, `license`, `metadata`,
`allowed-tools` — nothing else), the tree built in a private staging
sibling, and installed by the atomic directory no-clobber rename.
Bundled files copy verbatim (executable bits preserved for scripts);
multi-skill repositories install a chosen subset with per-skill
outcomes. Failures are typed (unreachable, not found, no skills
found, invalid skill, upstream rate-limited, changed-upstream).

#### Scenario: Install a marketplace skill

- **WHEN** the user previews and installs a skill from a public repo
- **THEN** it appears in the installed list (marketplace-badged) with
  its bundle intact and the connection to agents requiring no restart

#### Scenario: Duplicate refuses without clobbering

- **WHEN** a skill with the same name — or a distinct name whose slug
  collides with an installed or snapshot-invisible occupant — is
  installed
- **THEN** the install refuses and every existing file is untouched

#### Scenario: Multi-skill repositories

- **WHEN** a repository contains several skills
- **THEN** the preview lists them and the user installs a chosen
  subset with one outcome per skill

#### Scenario: An executable script survives

- **WHEN** an installed bundle contains an executable script
- **THEN** its executable bit survives preview, staging, and install
  without ever being invoked

#### Scenario: Registry-invalid skills reject at preview

- **WHEN** a skill's frontmatter carries fields the loader's strict
  decode rejects
- **THEN** the preview refuses it — the registry can never
  subsequently drop a previewed skill
