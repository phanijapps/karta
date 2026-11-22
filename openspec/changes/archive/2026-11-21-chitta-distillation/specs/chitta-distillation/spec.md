# Spec Delta

## Purpose

The distill-only write path of the evolution loop: how finished
conversations become durable memory and knowledge — selected,
extracted, validated, mapped, written atomically, idempotently, and
observably — with transcripts never persisted.

## ADDED Requirements

### Requirement: Distilled-only write path

The distiller SHALL be the only writer of session-derived content into
chitta, operating through the trusted seam with `source: distiller`
provenance naming the session and execution. Conversation storage
SHALL NOT be written or modified by the distiller, and raw transcripts
SHALL NOT be persisted in chitta — only distilled statements,
entities, and relationships.

#### Scenario: Session end alone writes nothing
- **WHEN** a session ends and the distiller has not run on it
- **THEN** chitta contains no content derived from that session

#### Scenario: Distillation writes carry distiller provenance
- **WHEN** the distiller persists an observation derived from session S
- **THEN** the record's provenance is `source: distiller` with session S named, and the content is the distilled statement, not transcript text

### Requirement: Session selection and checkpointing

The distiller SHALL select finished sessions that have not been
distilled, distilling each at most once per successful pass.
Checkpoint markers SHALL live in chitta's metadata (per session id,
with an attempt counter); the conversations database SHALL NOT gain
distiller state. A session whose extraction fails after the configured
attempt limit SHALL be marked dead-lettered and skipped until its
marker is cleared.

#### Scenario: A distilled session is not re-distilled
- **WHEN** the distiller runs again after session S distilled successfully
- **THEN** S is skipped with no new writes (idempotent re-runs)

#### Scenario: Repeated extraction failure dead-letters
- **WHEN** extraction for session S fails on the attempt limit
- **THEN** S is marked dead-lettered, later passes skip it, and the failure is observable in logs

### Requirement: Extraction contract

The distiller SHALL send each unprocessed session's transcript to the
configured provider and model with the distillation prompt, requiring
a single JSON object: observations (kind, subject, content, tags,
confidence, entities), entities (name, type, aliases), and
relationships (from, to, type) — bounded counts, confidence bands, and
the future-session-usefulness rule. Malformed or unparseable output
SHALL fail that session's attempt; nothing free-form SHALL reach
chitta. The prompt SHALL ship as a default and be overridable from the
configuration directory.

#### Scenario: Malformed LLM output fails the attempt
- **WHEN** the model returns unparseable or schema-invalid JSON
- **THEN** the session's attempt is counted and failed; no content is written to chitta

#### Scenario: Prompt override applies
- **WHEN** a distillation prompt file exists in the config directory
- **THEN** the distiller uses it instead of the embedded default

### Requirement: Mapping to chitta contracts

Extracted observations SHALL map onto chitta's closed kind enum and
tags (dot-notation keys and epistemic classes become tags; categories
map to kinds); entity types SHALL map onto chitta's closed entity-type
enum, with durable entity properties expressed as facts; episode
learnings become observations while session bookkeeping stays out of
chitta; procedures become `kind=procedure` observations carrying their
structured definition as content. The distiller SHALL NOT perform
overwrites or resurrections — it emits evidence, and chitta's
convergence and revision machinery decides.

#### Scenario: A distilled update converges instead of overwriting
- **WHEN** a session restates a known preference in different words
- **THEN** the existing record's confidence converges (or a new record is created); no key-based overwrite occurs

#### Scenario: A stale re-extraction is ignored
- **WHEN** extraction reproduces content matching a superseded record
- **THEN** the write disposition is `ignored_superseded` and the correction chain is untouched

### Requirement: Atomic per-session write

Each session's distilled output SHALL commit as one atomic
observation batch plus relationship connects; a failure on any item
rolls back the whole session's writes, leaving the session unmarked
for retry.

#### Scenario: Mid-batch failure leaves no partial state
- **WHEN** a storage failure occurs partway through a session's batch
- **THEN** no observation from that session persists and the session remains unprocessed

### Requirement: Distiller observability

Every trusted-seam write the distiller performs SHALL emit exactly one
`ChittaToolInvoked` event (tool name, affected ids, `source:
distiller` provenance); the tool path's emission SHALL remain exactly
one per call — no double emission for either path. Distiller pass
outcomes (sessions selected, distilled, failed, dead-lettered) SHALL
be logged.

#### Scenario: Distiller write emits one event
- **WHEN** the distiller persists a batch through the seam
- **THEN** a subscriber observes exactly one event per tool operation with distiller provenance

### Requirement: Configurable distillation model

The distillation provider and model SHALL be configurable at runtime
via the daemon-managed settings file and the Settings → Advanced
surface (applied without restart), with the prompt editable alongside.
Model changes SHALL NOT require data migration — re-distillation
idempotency makes them safe. The embedding model remains
config-file-only (unchanged).

#### Scenario: Model swap applies without restart
- **WHEN** the distillation model is updated through Settings → Advanced
- **THEN** the next distillation pass uses the new model and prior distillations remain valid
