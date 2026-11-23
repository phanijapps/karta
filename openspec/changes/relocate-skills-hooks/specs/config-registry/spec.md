# Spec Delta

## MODIFIED Requirements

### Requirement: Skill loading

The registry SHALL load skills from `skills/<name>/SKILL.md` directories (the top-level `~/.zbot/skills/` root), requiring Agent Skills frontmatter (`name`, `description`), and SHALL track each skill's bundled files verbatim for later injection.

#### Scenario: Skill with frontmatter loads

- **WHEN** `skills/pdf-tools/SKILL.md` has valid `name` and `description` frontmatter
- **THEN** the registry exposes the skill with that name, description, and directory

#### Scenario: Missing frontmatter reported

- **WHEN** a SKILL.md lacks `name` or `description`
- **THEN** the failure is reported with the file path and other skills still load

### Requirement: Hook manifest loading

The registry SHALL load `hooks/*.yaml` manifests (the top-level `~/.zbot/hooks/` root) with: `event` from the six lifecycle names (`PreToolUse`, `PostToolUse`, `PreCompact`, `SessionStart`, `SessionEnd`, `SubagentStop`), optional `matcher` (a regex that must compile), required `command`, and `timeout_ms`. Manifests failing any constraint are reported per-file.

#### Scenario: Valid manifest loads

- **WHEN** a hook manifest declares `event: PreToolUse`, a compiling `matcher`, a `command`, and a timeout
- **THEN** the registry exposes the hook bound to that event

#### Scenario: Bad event or matcher rejected

- **WHEN** a manifest names an unknown event or carries a non-compiling matcher regex
- **THEN** the failure is reported with the file path and the reason
