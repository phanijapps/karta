# Spec Delta

## Purpose

The file-driven registry: five loader kinds over the resolved home's config tree, with per-file error isolation and debounced fsnotify hot-reload, owning nothing hardcoded.

## ADDED Requirements

### Requirement: Prompt loading

The registry SHALL load every `config/prompts/*.md` file as a named Go text/template fragment (file stem is the prompt name), reporting per-file parse failures without aborting the load.

#### Scenario: Templates parse and are addressable

- **WHEN** `config/prompts/reviewer.md` contains a valid Go text/template
- **THEN** the registry exposes it under the name `reviewer`

#### Scenario: Broken template reported, others load

- **WHEN** one prompt file has invalid template syntax and another is valid
- **THEN** the valid prompt loads and the failure is reported with the invalid file's path

### Requirement: Agent spec loading

The registry SHALL load declarative agent specs from `config/agents/*.yaml` and from `*.md` files whose YAML frontmatter carries the spec, exposing name, role/system prompt (literal text or prompt-template reference), model overrides, and tool allowlists.

#### Scenario: YAML agent spec loads

- **WHEN** `config/agents/researcher.yaml` defines a valid spec
- **THEN** the registry exposes the agent named `researcher` with its prompt and tool settings

#### Scenario: Frontmatter agent spec loads

- **WHEN** `config/agents/researcher.md` carries the same spec in YAML frontmatter
- **THEN** it is exposed identically to the YAML form

#### Scenario: Duplicate names reported

- **WHEN** two agent files declare the same agent name
- **THEN** the conflict is reported with both file paths and the registry keeps a deterministic winner

### Requirement: Skill loading

The registry SHALL load skills from `config/skills/<name>/SKILL.md` directories, requiring Agent Skills frontmatter (`name`, `description`), and SHALL track each skill's bundled files verbatim for later injection.

#### Scenario: Skill with frontmatter loads

- **WHEN** `config/skills/pdf-tools/SKILL.md` has valid `name` and `description` frontmatter
- **THEN** the registry exposes the skill with that name, description, and directory

#### Scenario: Missing frontmatter reported

- **WHEN** a SKILL.md lacks `name` or `description`
- **THEN** the failure is reported with the file path and other skills still load

### Requirement: Hook manifest loading

The registry SHALL load `config/hooks/*.yaml` manifests with: `event` from the six lifecycle names (`PreToolUse`, `PostToolUse`, `PreCompact`, `SessionStart`, `SessionEnd`, `SubagentStop`), optional `matcher` (a regex that must compile), required `command`, and `timeout_ms`. Manifests failing any constraint are reported per-file.

#### Scenario: Valid manifest loads

- **WHEN** a hook manifest declares `event: PreToolUse`, a compiling `matcher`, a `command`, and a timeout
- **THEN** the registry exposes the hook bound to that event

#### Scenario: Bad event or matcher rejected

- **WHEN** a manifest names an unknown event or carries a non-compiling matcher regex
- **THEN** the failure is reported with the file path and the reason

### Requirement: MCP manifest loading

The registry SHALL load `config/mcps/*.yaml` manifests in two variants: stdio servers (`command` + `args` + `env`) and streamable HTTP servers (`url`), rejecting manifests that declare neither or both.

#### Scenario: Stdio MCP loads

- **WHEN** a manifest declares `command` with `args` and no `url`
- **THEN** the registry exposes a stdio MCP server entry

#### Scenario: Ambiguous variant rejected

- **WHEN** a manifest declares both `command` and `url`
- **THEN** the failure is reported with the file path

### Requirement: Per-file error isolation

A registry load SHALL always process every file in every config directory; failures SHALL be collected as typed errors carrying their file paths and SHALL NOT prevent valid entries in the same or other directories from loading. An empty control plane SHALL yield an empty registry with no errors.

#### Scenario: One bad file among many

- **WHEN** one of several hook manifests is invalid
- **THEN** the valid hooks load and exactly one error names the bad file

#### Scenario: Empty control plane

- **WHEN** all five config directories are empty
- **THEN** the registry loads empty with zero errors

### Requirement: Debounced hot-reload

The registry SHALL watch the resolved home's `config.yaml` and `config/**` with fsnotify, coalesce burst events with a debounce window, and reload only the affected entries — added and changed files reload, removed files unregister — notifying subscribers of each entry-level change. Registry notification SHALL be an in-process API, not a timeline event.

#### Scenario: Edited hook manifest reloads

- **WHEN** a loaded hook manifest file is modified while the watcher runs
- **THEN** the registry reloads that hook and notifies subscribers of the change

#### Scenario: Removed agent unregisters

- **WHEN** an agent spec file is deleted while the watcher runs
- **THEN** the registry unregisters the agent and notifies subscribers of the removal

#### Scenario: Editor save bursts coalesce

- **WHEN** a file write arrives as a burst of create+write events within the debounce window
- **THEN** the registry performs exactly one reload for that file
