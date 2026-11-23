# Spec Delta

## Purpose

The composed system prompt: static markdown sections from the prompt
registry, dynamic sections per surface, runtime skill provisioning,
and loud failures. Serves chat and goal runs.

## ADDED Requirements

### Requirement: The system prompt is composed from registry sections

The system prompt SHALL be composed from markdown section files in the
prompt registry (`~/.zbot/config/prompts/`). config.yaml selects them:
`system_prompt.role` (REQUIRED — identity and bounds), `.soul`
(optional — voice and principles), `.os` (`auto` or an explicit
`linux|windows|darwin|none`), `.tools` (optional — tool discipline),
and `.custom` (optional, an ordered list). `auto` SHALL select the
`os-<platform>` section by the daemon's operating system. Assembly
order SHALL be Role, Soul, OS, Tools, Custom. An install whose
config.yaml predates this key SHALL behave as if the defaults were
configured.

#### Scenario: Chat run composes the static sections

- **WHEN** a chat run starts with role, soul, os=auto (linux), and no
  custom sections configured
- **THEN** the run's system message carries Role, Soul, and os-linux
  content in that order

#### Scenario: Backward-compatible install

- **WHEN** config.yaml has no `system_prompt` key
- **THEN** the daemon uses the default section names and the run
  proceeds

### Requirement: Section failures are loud and typed

A missing or unresolvable REQUIRED section (role, or any named section
that cannot be read) SHALL be a typed load error naming the file and
the section. A missing OPTIONAL section SHALL be skipped without
error. The daemon SHALL NOT start a run with an accidentally empty
system prompt.

#### Scenario: Role section missing

- **WHEN** the configured role file does not exist in the registry
- **THEN** loading fails with an error that names the section and the
  file

#### Scenario: Optional section missing

- **WHEN** soul is configured but its file does not exist
- **THEN** the section is skipped and the run proceeds

### Requirement: Dynamic sections are appended per surface

After the static sections, machinery SHALL append dynamic sections at
run time: goal runs carry `## Task Analysis` (intent middleware) and
`## Skills` (provisioned skill bodies); goal runs recall `## Context`
from BOTH chitta domains (memory records and knowledge graph, fused).
Chat carries the static sections only in this change (its `## Context`
recall is a recorded follow-up). Slice 3 reserves an `## Active Ward`
section (the ward's own constitution).

#### Scenario: Goal run carries the full stack

- **WHEN** a goal run starts with a successful analysis
- **THEN** the system prompt carries the static sections, then
  `## Context`, `## Task Analysis`, and `## Skills` in that order

#### Scenario: Chat stays light

- **WHEN** a chat run starts
- **THEN** the system message carries the static sections and no Task
  Analysis, Skills, or Active Ward section

### Requirement: Skills are provisioned, not fetched

Skills SHALL reach goal-run context only through runtime provisioning:
the middleware validates recommended names against the registry and
inlines each validated skill's body into the `## Skills` section
before the run starts. There SHALL be no load_skill tool; the agent
does not fetch skills. An invalid recommendation SHALL be dropped with
a warning event.

#### Scenario: Validated skills are inlined

- **WHEN** the analysis recommends two skills that exist in the
  registry
- **THEN** both bodies appear in `## Skills` and the run starts

#### Scenario: Hallucinated name is dropped

- **WHEN** the analysis recommends a skill absent from the registry
- **THEN** the skill is not provisioned and a warning event records
  the drop

### Requirement: A fresh home seeds the default persona

Bootstrap SHALL seed default persona files into the prompt registry
(`role`, `soul`, `os-linux`, `os-windows`, `os-darwin`, `tools`),
write-once, under the same marker as the example roster: existing
files are never overwritten and deletions after the first seeding do
not resurrect.

#### Scenario: Fresh home seeds the persona

- **WHEN** bootstrap runs on a fresh home
- **THEN** the six persona files exist and the composed default prompt
  assembles without error

#### Scenario: User-owned persona survives re-bootstrap

- **WHEN** a user edits the seeded soul file and bootstrap runs again
- **THEN** the edit is preserved byte-for-byte
