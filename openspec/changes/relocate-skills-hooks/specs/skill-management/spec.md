# Spec Delta

## MODIFIED Requirements

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
