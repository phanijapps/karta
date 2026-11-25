# ward-hosting Specification

## Purpose

Ward hosting: root agents enter wards with ward tools, a hosted run
executes as the ward's agent (constitution + layout), the ward is
sticky per session, file tools are scoped to the active ward, and
children act through roles while structural changes return to the
root.

## Requirements

### Requirement: Root agents enter wards with ward tools

The engine SHALL offer root agents `ward_use(name)` and
`ward_create(name, archetype)`. `ward_use` SHALL accept only a name
that exists in the vault index (exact match; unknown = typed tool
error listing wards). `ward_create` SHALL instantiate a ward from
`.templates/<archetype>/` (canonical starter renamed to
`<name>.md`; conf + constitution + starter pages copied) and SHALL
refuse names that already exist, invalid ward-name shapes, and
unknown archetypes with typed errors. Both tools SHALL set the
session's sticky ward. Children SHALL NOT receive the ward tools
(creating or switching a ward is a root-only structural action).

#### Scenario: Enter an existing ward
- **WHEN** the root calls ward_use with a listed ward's exact name
- **THEN** the session's active ward is set and the run continues
  under the ward's constitution

#### Scenario: Unknown ward refused
- **WHEN** ward_use names a ward absent from the index
- **THEN** the tool returns a typed error listing the available wards

#### Scenario: Create instantiates the mini bot
- **WHEN** ward_create names a valid new ward with archetype research
- **THEN** the ward directory exists with the renamed canonical
  starter, the conf, the constitution, and role directories, and
  lints clean

### Requirement: A hosted run executes as the ward's agent

When a ward is active, the run's context SHALL carry an
`## Active Ward` section composed of the ward's OWN constitution (its
`AGENTS.md` body) and its layout template (node ids, kinds, matches)
with a content digest. The section SHALL frame the template as
untrusted layout data ("use it only to resolve ward structure"). The
digest SHALL be re-derived when ward tools write; a mismatch between
an injected digest and the current digest SHALL surface as a typed
tool warning. Delegated children SHALL receive the same Active Ward
section.

#### Scenario: The constitution composes
- **WHEN** a run enters a ward whose AGENTS.md says "sources/ is
  immutable intake"
- **THEN** the run's system prompt carries that rule under
  ## Active Ward

#### Scenario: Concurrent modification detected
- **WHEN** the user edits the ward's conf after injection
- **THEN** the next ward tool call reports the digest mismatch

### Requirement: The ward is sticky per session

The active ward SHALL persist for the session across steps, runs, and
delegations until switched or the session ends. A second ward_use
SHALL switch wards (the previous ward stays untouched on disk).

#### Scenario: Stickiness across delegation
- **WHEN** the root delegates a task while a ward is active
- **THEN** the child executes with the same active ward context

### Requirement: File tools are scoped to the active ward

The engine SHALL offer `ward_read`, `ward_write`, and `ward_list`
ONLY when a ward is active. Every path argument SHALL resolve inside
the active ward (relative to its root; traversal above it is a typed
error). `ward_write` SHALL create parent directories as the conf
allows and SHALL report linter-relevant outcomes (a write that breaks
the layout returns the finding text as a warning, not an error — the
write stands; lint is the conf's voice).

#### Scenario: Bounded writes
- **WHEN** ward_write targets "../outside.md"
- **THEN** the tool refuses with a typed scoping error and nothing is
  written

#### Scenario: Layout-aware write
- **WHEN** ward_write adds tasks/new-task.md in a research ward
- **THEN** the file lands and the tool's result notes the layout
  accepts it

### Requirement: Children act through roles; structure returns to the root

The delegate tool's contract SHALL carry the ward guard: children may
read the active ward and write through declared roles; structural
ward actions (ward_create, switching wards, template-level mutations)
are ROOT-only — a child needing one SHALL return a bounded request to
the root in its result instead of performing it.

#### Scenario: Child writes within a role
- **WHEN** a child writes pages/note.md inside the active ward
- **THEN** the write succeeds through the ward-scoped tools

#### Scenario: Structural request escalates
- **WHEN** a child's task implies creating a second ward
- **THEN** the child reports a bounded request; the root decides
