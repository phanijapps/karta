# Spec Delta

## Purpose

The transfer objects and wire rules every chitta participant exchanges —
tools, the domain seam, storage providers, and future MCP adapters — so the
same shapes flow unchanged across in-process calls and process boundaries.

## ADDED Requirements

### Requirement: Memory record object

A memory record SHALL carry the fields below; optional-absent fields are
omitted, never null. `kind` is the closed enum `fact | preference |
correction | observation | procedure`; `subject` is an open string
naming who/what the record is about (default `user`); `confidence` is
in [0,1]; `status` is `active | superseded`. `mention_count` and
`last_confirmed_at` track re-confirmation for the evolution loop.

```json
{
  "id": "mem_01J8ZG3W2K...",
  "kind": "preference",
  "subject": "user",
  "content": "Prefers concise answers with code examples",
  "tags": ["communication"],
  "confidence": 0.9,
  "status": "active",
  "supersedes": "mem_01J8ZF...",
  "entities": ["ent_01J8ZG9..."],
  "provenance": { "source": "agent", "session_id": "s1", "execution_id": "e1", "agent_id": "root", "message_seq": 42 },
  "mention_count": 3,
  "last_confirmed_at": "2026-11-08T10:00:00Z",
  "created_at": "2026-11-08T10:00:00Z",
  "updated_at": "2026-11-08T10:00:00Z"
}
```

#### Scenario: Round-trip preserves all fields
- **WHEN** a record is written and then read back through any surface (tool, seam, provider)
- **THEN** every field above is present and unchanged, with optional-absent fields omitted rather than null

### Requirement: Provenance is host-stamped

The `provenance` object SHALL record `source` (`agent`, `user`, or
`distiller`), plus the session id, execution id, agent id, and message
sequence number available at write time. Callers SHALL NOT be able to
supply or alter provenance through any write surface; the host stamps it at
the boundary. Reads return provenance verbatim.

#### Scenario: Agent write cannot forge origin
- **WHEN** an agent-supplied write payload contains a `provenance` field
- **THEN** the field is rejected as unknown input and the stored provenance reflects the actual calling context

### Requirement: Evidence and confirmation semantics

The host SHALL stamp every accepted observation with a contribution
identity (source, session, execution, message sequence, normalized
content hash). Replaying an identical contribution SHALL be idempotent:
no confidence change, no new contribution. A distinct contribution
matching an active record SHALL advance `mention_count` and
`last_confirmed_at` and converge confidence as `c' = c + (1 − c) · α`
with `α` configurable (default 0.3) — confidence approaches but never
reaches 1.0 by repetition alone. Contributions SHALL be retained and
auditable through the domain seam so the evolution loop's evidence is
reconstructable.

#### Scenario: Replay of identical evidence is idempotent
- **WHEN** the same observation (same contribution identity) is delivered twice
- **THEN** the second delivery changes no confidence, mention, or contribution entry

#### Scenario: Distinct evidence converges confidence
- **WHEN** the same content is observed from two different sessions
- **THEN** confidence rises toward but not to 1.0 per the formula, and both contributions are retained

### Requirement: Correction chain semantics

A correction SHALL create a new record whose `supersedes` names the old
record id, and SHALL flip the old record's `status` to `superseded`
with `superseded_by` pointing back. The new record SHALL inherit the
superseded record's `kind` unless an explicit kind is supplied — the
chain, not the kind, marks the correction. Chains SHALL be followable
in both directions, and reads and recall SHALL exclude `superseded`
records unless explicitly requested.

#### Scenario: Correcting a preference
- **WHEN** a correction supersedes an active record and the full history is listed
- **THEN** the old record shows `status=superseded` with `superseded_by` set, and the new record shows `supersedes` naming the old id and inherits its kind

#### Scenario: Superseded records hidden by default
- **WHEN** a superseded record exists and recall/list runs without an include flag
- **THEN** the superseded record is absent from results but retrievable via the history `ids` filter

### Requirement: Corrections are never resurrected

An observation whose normalized content exactly matches a superseded
record SHALL NOT create or reactivate any record: the disposition SHALL
be `ignored_superseded` and the revision chain remains authoritative.

#### Scenario: Stale replay of a corrected fact is ignored
- **WHEN** a preference was revised and a later observation replays the old content exactly
- **THEN** no new active record is created, the disposition is `ignored_superseded`, and the correction chain is unchanged

### Requirement: Distilled-only write paths

Chitta SHALL accept writes through exactly two paths: the agent/user
tool surface, and trusted host operations on the seam. Conversation
storage SHALL NOT write chitta directly: conversation content reaches
chitta only as distilled artifacts (observations, entities,
relationships) from the distillation job, carrying `source: distiller`
provenance naming the originating session and execution. Raw
transcripts SHALL NOT be persisted in chitta.

#### Scenario: Session end alone writes nothing
- **WHEN** a session ends and the distillation job has not run
- **THEN** chitta contains no record of that session's content

#### Scenario: Distilled records carry distiller provenance
- **WHEN** the distillation job persists an observation derived from session S, execution E
- **THEN** the record's provenance is `source: distiller` with S/E named, and its content is the distilled statement, not the raw transcript

### Requirement: Entity object

An entity SHALL carry: a server-assigned id (prefixed `ent_`), a closed
`type` enum (`person`, `project`, `tool`, `organization`, `place`,
`concept`, `other`), a display `name`, a `normalized_name` (NFC, casefold,
whitespace-collapsed), zero or more `aliases` (normalized on write), a
`confidence`, a `mention_count`, and `last_mentioned_at`.

#### Scenario: Alias normalization on write
- **WHEN** an entity is upserted with alias `"  PostGres "`
- **THEN** the stored alias is the normalized form `postgres` and matches later name references of any casing

### Requirement: Relationship object

A relationship SHALL carry: an id (prefixed `rel_`), `from` and `to`
entity ids, a snake_case verb `type` (open vocabulary, e.g. `works_on`,
`prefers`, `located_in`), a `confidence`, `provenance`, and `created_at`.

#### Scenario: Relationship endpoints are entity ids
- **WHEN** a relationship created via name references is read back
- **THEN** its `from` and `to` fields contain resolved entity ids, not the input names

### Requirement: Name-or-id references

Entity and relationship references in write inputs MAY be given as ids or
as names/aliases; the host SHALL resolve names via normalized match or
create the entity when no match exists.

#### Scenario: Name reference resolves to existing entity
- **WHEN** a relationship write references `"Postgres"` and an entity exists with alias `postgres`
- **THEN** the relationship links to that entity's id without creating a duplicate

### Requirement: Observation batch and disposition

A `memory_remember` input SHALL be a bounded batch of observation
objects: `kind`, `content`, optional `subject` (default `user`), `tags`,
`confidence`, and optional `entities` (names or ids). The output SHALL
return one result per observation: the stored memory record plus a
`disposition` of `created` or `merged`. Validation SHALL be
all-or-nothing: one invalid observation rejects the batch with no
partial writes.

#### Scenario: Batch preserves order
- **WHEN** a multi-observation batch is remembered
- **THEN** results return one-per-observation in input order, each with its own disposition

### Requirement: Dossier object

A `knowledge_about` output SHALL contain: an optional resolved `entity` (with
aliases) or a `subject` summary, a `records` page of active memory records
referencing the target, and a `neighborhood` of entities and relationships
within the requested depth. Every component SHALL be empty-able rather
than null when nothing matches.

#### Scenario: Dossier composes its parts
- **WHEN** a target resolves to an entity with two referencing records and one neighbor
- **THEN** the dossier carries the entity, a two-item records page, and a one-relationship neighborhood

### Requirement: Recall request

A recall request SHALL accept: a textual `query`, optional filters
(`kinds`, `tags`, `subject`, `entity_ids`, `since`, `until`), a `limit`
(default 10, maximum 100 — beyond is `invalid_input`), and an optional
`include_superseded` flag.

#### Scenario: Limit bounds enforced
- **WHEN** a recall request sets `limit` to 101
- **THEN** the request fails with `invalid_input` naming the bound

### Requirement: Recall result

A recall result SHALL be a page of hits, each hit carrying the record,
a `score`, and the matched `lanes` (`keyword`, `vector`, `graph` — at
least one). Vector matching engages only when the host supplies a query
embedding and the record carries one; its absence SHALL NOT fail the
request.

```json
{ "items": [ { "record": { "...": "memory record object" }, "score": 0.83, "lanes": ["keyword", "graph"] } ], "next_cursor": "..." }
```

#### Scenario: Lane attribution present
- **WHEN** a hit matched full-text content and also references a filtered entity
- **THEN** the hit's `lanes` contains both `keyword` and `graph`

### Requirement: Page envelope and cursor

All list-shaped outputs (recall, listing, entity search, graph results)
SHALL use one envelope: `items` plus an optional opaque `next_cursor`.
An absent `next_cursor` means the end of the result set. Cursors SHALL be
stable across pagination for a fixed underlying dataset and SHALL NOT
leak internal state.

#### Scenario: Full pagination walk
- **WHEN** results exceed one page and the consumer follows `next_cursor` until absent
- **THEN** every matching item is returned exactly once, in the same order as an unbounded request

### Requirement: Error envelope and codes

Every failing operation SHALL return `{ "error": { "code": "...",
"message": "...", "detail": ... } }` with a closed code set:
`invalid_input`, `not_found`, `conflict`, `unavailable`. Unknown or
malformed write fields SHALL be `invalid_input`; missing referenced
records SHALL be `not_found`; duplicate creation constraints SHALL be
`conflict`; provider/database unavailability SHALL be `unavailable`.

#### Scenario: Missing record read
- **WHEN** a read names a record id that does not exist
- **THEN** the response is `not_found` with the id in the message, not an empty success

### Requirement: Wire serialization rules

All transfer objects SHALL serialize as JSON objects with snake_case field
names, RFC3339 UTC timestamps, closed string enums, and optional-absent
fields omitted (never null). Writes SHALL reject unknown fields; reads
SHALL tolerate them. Evolution SHALL be additive-only — new optional
fields, new enum values only via a contract revision — never renames,
removals, or type changes.

#### Scenario: Unknown write field rejected
- **WHEN** a write payload includes a field not in the contract
- **THEN** the operation fails with `invalid_input` naming the field

#### Scenario: Forward-compatible read
- **WHEN** a payload from a newer producer carries an unknown field and is only read
- **THEN** the unknown field is ignored and the known fields deserialize without error
