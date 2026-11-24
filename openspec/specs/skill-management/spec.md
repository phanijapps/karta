# skill-management Specification

## Purpose

Daemon-managed lifecycle for workspace skills: CRUD over `config/skills/<dir>/SKILL.md` directories with frontmatter validation, slug-directory rules, and bundle-inclusive deletion and rename semantics.

## Requirements

### Requirement: Skill directories and frontmatter

A skill SHALL be one directory under `skills/` (the top-level `~/.zbot/skills/` root) containing a `SKILL.md` whose YAML frontmatter carries `name` and `description` (both required, non-empty) and whose body is markdown. The directory name SHALL be the slugified skill name, enforced on create and rename; entries with other directory names remain loadable and manageable in place. Skill files beyond `SKILL.md` are the skill's bundle and are opaque to management: they are preserved verbatim by every operation that keeps the skill. Authored frontmatter SHALL NOT influence a skill's filesystem location — management targets are derived from the skills root and the slug, never from frontmatter-supplied paths.

#### Scenario: Created skill matches the loader

- **WHEN** a skill is created with a name, description, and markdown body
- **THEN** a `SKILL.md` exists in the slugified directory, loads through the registry's skill loader without error, and is listed with that name and description

#### Scenario: Missing description rejected

- **WHEN** a create or edit supplies a name but an empty description
- **THEN** the mutation is rejected with a validation error and no directory changes

#### Scenario: Authored path is ignored

- **WHEN** a SKILL.md carries a `path` (or other sink) frontmatter key
- **THEN** the skill manages at its directory-derived location and the authored value never becomes a target


### Requirement: Skill CRUD applies immediately

Create SHALL write the directory and `SKILL.md` atomically (no reader observes a half-written skill) using no-clobber installation — an occupied target path fails as a duplicate whatever occupies it, including directories the loader skips. Edit, rename, and delete SHALL behave per scenario, and every completed mutation SHALL be reflected in the registry snapshot before the API response is sent; after any failed mutation the published registry state SHALL match what is on disk. Hand-edited skill directories keep working through hot-reload.

#### Scenario: Create then list

- **WHEN** a skill is created via the API
- **THEN** the create response succeeds and an immediate list includes the skill with no daemon restart

#### Scenario: Occupied destination conflicts

- **WHEN** a create or rename targets a directory that already exists on disk, including assets-only or load-invalid ones absent from the snapshot
- **THEN** the mutation fails with a duplicate error and neither directory changes

#### Scenario: Edit preserves the bundle

- **WHEN** a skill with bundled files has its description edited
- **THEN** the bundle files are unchanged and the skill loads with the new description

#### Scenario: Rename moves the directory

- **WHEN** a skill is renamed
- **THEN** the directory moves to the new slug, the frontmatter name matches, and the bundle travels with it

#### Scenario: Delete removes the bundle

- **WHEN** a skill holding bundled files is deleted
- **THEN** the skill's directory and every bundled file are gone and the skill is no longer listed

#### Scenario: Duplicate name across directories rejected

- **WHEN** a skill is created whose name matches another skill in a different directory
- **THEN** the mutation fails with a duplicate-name error and neither directory changes

#### Scenario: Failed rename leaves a disk-true state

- **WHEN** a rename fails after the directory has moved
- **THEN** the registry publishes the state actually on disk, the skill remains loadable (possibly under its prior name), and a typed error is returned

#### Scenario: Partially failed delete is visible

- **WHEN** a delete removes some but not all of a skill's directory
- **THEN** the registry publishes the state actually on disk, the residual is reported as a load error or an assets-only directory, and a typed error is returned


### Requirement: Skill body is retrievable

The daemon SHALL expose a skill's full `SKILL.md` content (frontmatter values and markdown body) for reading, so clients can present and edit the authored skill without filesystem access. Reading SHALL NOT alter the skill or its load state.

#### Scenario: Read a skill for editing

- **WHEN** a skill's detail is requested
- **THEN** the name, description, and markdown body are returned as currently loaded
