# config-registry Specification

## Purpose

The file-driven registry: five loader kinds over the resolved home's config tree, with per-file error isolation and debounced fsnotify hot-reload, owning nothing hardcoded.

## Requirements

### Requirement: Prompt loading

The registry SHALL load every `config/prompts/*.md` file as a named Go text/template fragment (file stem is the prompt name), reporting per-file parse failures without aborting the load.

#### Scenario: Templates parse and are addressable

- **WHEN** `config/prompts/reviewer.md` contains a valid Go text/template
- **THEN** the registry exposes it under the name `reviewer`

#### Scenario: Broken template reported, others load

- **WHEN** one prompt file has invalid template syntax and another is valid
- **THEN** the valid prompt loads and the failure is reported with the invalid file's path

### Requirement: Agent spec loading

The registry SHALL load declarative agent specs from `~/.zbot/agents/*.yaml` and from `*.md` files whose YAML frontmatter carries the spec, exposing name, role/system prompt (literal text or prompt-template reference), description, model overrides, tool allowlists, and max_depth. Bootstrap SHALL seed example agents there and SHALL one-time-migrate files from the legacy `config/agents` location when the new directory is absent — user-owned files are never overwritten.

#### Scenario: YAML agent spec loads

- **WHEN** `~/.zbot/agents/researcher.yaml` defines a valid spec
- **THEN** the registry exposes the agent named `researcher` with its prompt and tool settings

#### Scenario: Frontmatter agent spec loads

- **WHEN** `~/.zbot/agents/researcher.md` carries the same spec in YAML frontmatter
- **THEN** it is exposed identically to the YAML form

#### Scenario: Duplicate names reported

- **WHEN** two agent files declare the same agent name
- **THEN** the conflict is reported with both file paths and the registry keeps a deterministic winner

#### Scenario: Legacy location migrates once

- **WHEN** bootstrap runs with `config/agents` populated and `~/.zbot/agents` absent
- **THEN** the legacy files move to `~/.zbot/agents` and later edits there survive re-bootstrap

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

### Requirement: MCP manifest variants

The registry SHALL load `config/mcps/*.yaml` manifests in three variants: stdio servers (`command` + `args` + `env`), streamable HTTP servers (`url`), and dual-transport servers declaring both — one entry carrying two transports. Manifests declaring neither transport SHALL be rejected.

#### Scenario: Stdio MCP loads

- **WHEN** a manifest declares `command` with `args` and no `url`
- **THEN** the registry exposes a stdio MCP server entry

#### Scenario: Dual-transport manifest loads

- **WHEN** a manifest declares both `command` and `url`
- **THEN** the registry exposes one entry carrying both transports

#### Scenario: Empty manifest rejected

- **WHEN** a manifest declares neither `command` nor `url`
- **THEN** the failure is reported with the file path

### Requirement: Serialized write paths for managed kinds

The registry SHALL provide mutations (create, update, rename, delete) for the MCP and skill kinds, validated by the same rules as loading (at-least-one transport, frontmatter requirements, duplicate-name rejection, managed-name and slug rules, enforced on create and rename targets). Writes SHALL be atomic (temp-file plus rename; never a partial manifest or SKILL.md), installed with no-clobber semantics, and SHALL publish the updated snapshot synchronously before the mutation returns, delivering the matching added/changed/removed notifications themselves. Mutations and watcher-triggered reloads SHALL be serialized so an external change can never publish a stale snapshot over a completed mutation; an external edit landing after a mutation reloads the mutated kind from disk, preserving the last writer's intent. After a failed mutation the registry SHALL publish the state actually on disk. Creates SHALL be capacity-bounded per kind; files overflowing the capacity at load SHALL be reported as load errors. Mutations for prompts, agents, and hooks SHALL NOT exist (they remain file-authored).

#### Scenario: Mutation publishes synchronously

- **WHEN** a mutation completes
- **THEN** the snapshot a reader takes immediately afterwards reflects the mutation

#### Scenario: Mutation notifies subscribers

- **WHEN** a mutation completes and the watcher later settles on the written file
- **THEN** subscribers receive the mutation's own added/changed/removed notification exactly once and the settle emits nothing further

#### Scenario: Watcher never reverts a mutation

- **WHEN** an external file event for the mutated kind races the mutation
- **THEN** the final published snapshot matches disk state after both, and never reverts the completed mutation

#### Scenario: Validation rejects before writing

- **WHEN** a mutation violates a load rule
- **THEN** it fails with a typed error and no file is written or replaced

#### Scenario: Failed mutation publishes disk state

- **WHEN** a mutation fails partway through its filesystem changes
- **THEN** the published snapshot and errors reflect what is actually on disk, never an in-memory expectation

#### Scenario: Capacity overflow is a load error

- **WHEN** hand-edited files push a managed kind beyond its capacity at load
- **THEN** the lexicographically-last excess files are reported as load errors and the rest load normally

### Requirement: Load-error visibility

The registry SHALL retain the most recent load's per-file errors for every kind and expose them to readers alongside the current snapshot, refreshed on initial load and on every watcher-triggered reload, so a hand-edited file's breakage is queryable without reading logs. Errors describe the current snapshot: an entry whose file no longer loads is absent from the snapshot, and its error names the file that went away.

#### Scenario: Broken hand-edit is queryable

- **WHEN** a hand-edited manifest that previously loaded becomes invalid during a watcher reload
- **THEN** the reader-visible errors name that file's path and the snapshot reflects the currently loadable entries
