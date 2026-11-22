# Spec Delta

## Purpose

The agent-facing tool surface of the chitta capability — the core the
harness uses to continuously evolve. Two first-class domains, two tool
namespaces: `memory_*` (experiential records about the user) and
`knowledge_*` (the entity/relationship graph of the user's world) —
identical in-process and over MCP.

## ADDED Requirements

### Requirement: Memory tools

The chitta capability SHALL expose exactly four memory-domain tools:
`memory_remember` (bounded batch of observations; exact-duplicate merge;
entity auto-linking), `memory_recall` (ranked hybrid retrieval),
`memory_revise` (supersede one active record; sole retirement path), and
`memory_history` (chronological audit page, superseded visible by
default).

#### Scenario: Save then recall
- **WHEN** `memory_remember` persists a fact and `memory_recall` queries its content
- **THEN** the saved record appears in the result page with lane attribution

### Requirement: Knowledge tools

The chitta capability SHALL expose exactly three knowledge-domain tools:
`knowledge_connect` (idempotent typed relationship upsert; endpoints
resolved or created by name), `knowledge_find_entities` (prefix/type
entity search), and `knowledge_about` (dossier bridge: knowledge anchor,
memory records, bounded neighborhood).

#### Scenario: Tool inventory is complete and closed
- **WHEN** a client enumerates the chitta tools
- **THEN** exactly the seven tools above are listed across the two namespaces, each with its schema

### Requirement: Remember tool semantics

`memory_remember` batches SHALL be bounded and validated all-or-nothing.
Normalized content SHALL be NFC, casefolded, whitespace-collapsed — the
same normalization as entity names. Merge, confirmation, and replay
behavior SHALL follow the evidence and confirmation semantics in the
chitta-contracts capability (dispositions `created`, `merged`,
`ignored_superseded`).

#### Scenario: Exact duplicate merges, not duplicates
- **WHEN** an observation's subject, kind, and normalized content exactly match an active record, from distinct evidence
- **THEN** the disposition is `merged`, confidence converges per the contracts formula, `mention_count` and `last_confirmed_at` advance, and no duplicate record is created

#### Scenario: Invalid observation rejects the batch
- **WHEN** a batch contains one observation with an unknown enum value
- **THEN** the whole batch fails with `invalid_input` and no records are written

#### Scenario: Mentioned entity auto-links
- **WHEN** an observation references `Postgres` and an entity with alias `postgres` exists
- **THEN** the stored record lists that entity's id without any explicit entity argument

### Requirement: Corrections are never resurrected

An observation whose normalized content exactly matches a superseded
record SHALL NOT create or reactivate any record: the disposition SHALL
be `ignored_superseded`, the revision chain remains authoritative, and
the match is recorded as a mention for audit.

#### Scenario: Stale replay of a corrected fact is ignored
- **WHEN** a preference was revised and a later observation replays the old content exactly
- **THEN** no new active record is created, the disposition is `ignored_superseded`, and the correction chain is unchanged

### Requirement: Recall tool semantics

`memory_recall` SHALL answer a textual query with a ranked page of hits
(record plus `score` plus matched `lanes`), filtered by kinds, tags,
subject, entity, and time range. `limit` SHALL default to 10 and be
capped at 100 (`invalid_input` beyond). Keyword and graph lanes SHALL
always be available; the vector lane engages only when the host supplies
a query embedding. Superseded records are excluded unless opted in.

#### Scenario: Keyword-only recall works without embeddings
- **WHEN** no records carry embeddings and `memory_recall` is called
- **THEN** results rank by keyword matching and hits attribute the `keyword` and/or `graph` lanes only

### Requirement: Revise tool semantics

`memory_revise` SHALL replace one active record: it creates a new record
whose `supersedes` names the old id and flips the old record to
`superseded` with `superseded_by` set. The new record SHALL inherit the
superseded record's `kind` unless an explicit `kind` is supplied — a
corrected preference remains a preference. Revising a superseded record
SHALL fail with `conflict`.

#### Scenario: Revision chain is followable
- **WHEN** a record is revised and the pair is read back
- **THEN** the old record shows `superseded`/`superseded_by` and the new shows `supersedes`, each pointing at the other

#### Scenario: Corrected preference stays a preference
- **WHEN** a `preference` record is revised without an explicit kind and `memory_recall` filters to kind `preference`
- **THEN** the revision is discoverable under that filter; the chain, not the kind, records the correction

#### Scenario: Revising a superseded record fails
- **WHEN** `memory_revise` targets a record already superseded
- **THEN** the call fails with `conflict` and no records change

### Requirement: History tool semantics

`memory_history` SHALL return a chronological page of records filtered
by subject, kinds, tags, time range, and/or an explicit `ids` list (the
direct-id retrieval surface), including superseded records with their
`status` visible by default, with an active-only opt-out and a cursor.

#### Scenario: Audit view shows superseded records
- **WHEN** a record was revised and `memory_history` runs for its subject
- **THEN** both the superseded original and the revision appear, each with its `status`

#### Scenario: Direct id retrieval
- **WHEN** `memory_history` is called with an `ids` list containing a superseded record's id
- **THEN** that record is returned with its `status` and chain fields

### Requirement: Connect tool semantics

`knowledge_connect` SHALL be idempotent on `(from, to, type)` — a repeat
bumps confidence and mention metadata instead of duplicating — and SHALL
create missing endpoint entities by name.

#### Scenario: Connect by names creates and links once
- **WHEN** `knowledge_connect` links two names that have no entities yet, twice
- **THEN** both entities exist after the first call, one relationship exists, and the second call returns the same relationship id

### Requirement: Entity search tool semantics

`knowledge_find_entities` SHALL return a page of entities matching a
prefix (over names and aliases) and optional type filter, ordered by
match rank then name then id.

#### Scenario: Alias prefix matches
- **WHEN** an entity has alias `Postgres` and `knowledge_find_entities` queries prefix `postg`
- **THEN** the entity is returned

### Requirement: Dossier tool semantics

`knowledge_about` SHALL resolve one target as an entity (name, alias, or
id) first, else as a subject, and return: the entity with aliases or the
subject summary, a page of active memory records referencing the target
(ordered by `created_at` descending then id), and the knowledge
neighborhood to a bounded depth (1–3, default 1; out-of-range depth is
`invalid_input`). An unresolvable target SHALL return an empty dossier,
not an error.

#### Scenario: Orientation on a known entity
- **WHEN** `knowledge_about` targets an entity with memories and relationships
- **THEN** the dossier returns the entity with aliases, the referencing active records, and its neighborhood within the requested depth

#### Scenario: Unknown target is an empty dossier
- **WHEN** `knowledge_about` targets a name with no entity or subject match
- **THEN** the response is an empty dossier with no error

#### Scenario: Neighborhood respects depth bound
- **WHEN** a chain A→B→C exists and `knowledge_about` requests depth 1 from A
- **THEN** B appears in the neighborhood and C does not

### Requirement: Host-stamped trust boundary

Tool inputs SHALL NOT accept `provenance`, `id`, `status`, `created_at`,
`updated_at`, `mention_count`, `last_confirmed_at`, or `embedding`;
these are host-assigned. Agent-suggested `confidence` SHALL be clamped
to [0,1]. Supplying reserved fields SHALL fail with `invalid_input`
naming the field.

#### Scenario: Reserved fields rejected
- **WHEN** `memory_remember` is called with an `id`, `provenance`, or `embedding` field
- **THEN** the call fails with `invalid_input` naming the reserved field

### Requirement: Agents never delete

No tool SHALL hard-delete records or entities. The sole record status
transition SHALL be active → superseded via `memory_revise`.

#### Scenario: No deletion path exists
- **WHEN** the tool inventory is inspected for any delete or forget operation
- **THEN** none exists

### Requirement: Deterministic results

Every read tool SHALL define and apply a total order, identical across
calls against identical state: `memory_recall` by score then recency
then id; `memory_history` by `created_at` then id;
`knowledge_find_entities` by match rank then name then id;
`knowledge_about` record pages by `created_at` descending then id.

#### Scenario: Repeat recall is stable
- **WHEN** the same recall request is issued twice with no writes between
- **THEN** both responses list the same records in the same order with the same scores

#### Scenario: Full pagination walk is exact
- **WHEN** `memory_history` results exceed one page and the consumer follows `next_cursor` until absent
- **THEN** every matching item is returned exactly once in the tool's total order

#### Scenario: Recall and entity search are top-k
- **WHEN** `memory_recall` or `knowledge_find_entities` returns more potential matches than `limit`
- **THEN** the page contains the top `limit` results in the tool's total order with no `next_cursor` (v1: recall and entity search are top-k; cursor support is a contract-revision follow-up)

### Requirement: Transport neutrality

Tool names, input schemas, and output shapes SHALL be identical across
in-process engine binding and MCP exposure. Every tool SHALL be a single
request/response exchange — no streaming, callbacks, or ambient state.
Tool names SHALL use underscore namespaces (no dots).

#### Scenario: Same schema in-process and over MCP
- **WHEN** the same tool is invoked through the in-process binding and an MCP transport
- **THEN** both accept identical input JSON and return the identical output envelope

### Requirement: Every tool call is observable

Each tool invocation SHALL emit one `ChittaToolInvoked` event carrying
the tool name, outcome, error code on failure, affected ids, and host
provenance. Publication SHALL be attempted for every call including
failures; a failed publication SHALL NOT fail the tool call.

#### Scenario: Failed call is still observable
- **WHEN** `memory_remember` fails validation
- **THEN** a `ChittaToolInvoked` event records the tool, the `invalid_input` code, and no affected record id
