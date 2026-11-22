# Spec Delta

## REMOVED Requirements

### Requirement: MCP manifest loading

**Reason:** Transport exclusivity (rejecting manifests that declare both `command` and `url`) is superseded — MCP servers may now be reachable both ways, so one manifest may declare both transports.

**Migration:** Manifests previously rejected for declaring both `command` and `url` now load as single dual-transport entries; no file changes are required.

## ADDED Requirements

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
