# Spec Delta

## MODIFIED Requirements

### Requirement: Intent analysis precedes root runs

*(from add-intent-analysis — the analysis flow itself is reworked;
the pre-execution position, root-only scope, and non-fatal
degradation are unchanged.)*

The daemon SHALL analyze the goal before root research runs: a
deterministic router bypasses trivial goals, and otherwise ONE
packet-driven decision call classifies the goal (intent, complexity,
approach, ward recommendation, recommended skills/agents/MCPs) with
bounded latency. Analysis NEVER blocks or fails the run — degradation
is typed and carries its reason.

#### Scenario: The packet is host-authoritative and memory-ranked

- **WHEN** the analyzer builds its context
- **THEN** the prompt carries a catalog packet built from the live
  registry/index (skills, agents, MCPs, wards with names and
  descriptions), ordered by chitta memory recall and truncated per
  kind, with the user request embedded as escaped untrusted data

#### Scenario: Correction feedback, not blind retries

- **WHEN** the decision fails parsing or names resources absent from
  the packet
- **THEN** the retry prompt states the failure code and the fix and
  the analyzer re-asks (at most three attempts inside a 45-second
  deadline) before degrading typed

#### Scenario: Provider-aware completion parameters

- **WHEN** the analysis model is a z.ai glm-5.3* model
- **THEN** the completion request carries `reasoning_effort: low`
  (analysis is a classification, not a reasoning task) and other
  providers are unaffected

#### Scenario: Trivial goals still bypass

- **WHEN** the goal is conversational traffic (router bypass)
- **THEN** no analysis call runs and the event carries the bypass
  reason (unchanged)

### Requirement: The analysis is queryable session state

The daemon SHALL expose the active session's LATEST intent analysis
as session state (`GET /api/sessions/{id}/state`): the session row
plus the analysis block (status, reason, primary intent, complexity,
budget, approach, ward action/name, recommended skills/agents,
dropped names). The chat surface's Task Analysis card and the context
panel's Provisioned Skills section SHALL render from this state (or
the live event) — never by parsing transcript rows.

#### Scenario: The card renders for engine-routed research runs

- **WHEN** a research session's state carries an analyzed intent
- **THEN** the chat surface renders the Task Analysis card with the
  intent, complexity, approach, ward, provisioned skills, and dropped
  names

#### Scenario: Chat sessions render no analysis

- **WHEN** the active session is the quick chat (lean — no analysis
  runs)
- **THEN** session state carries no analysis block and the surface
  renders no card

#### Scenario: The draft clears at send start

- **WHEN** the user sends a message
- **THEN** the composer's draft clears immediately (not on
  completion) — a multi-minute research run never holds the user's
  text in the input

### Requirement: Final answers persist to the transcript

Every root run's final model answer SHALL append to the session's
transcript as the final assistant message row; a failure to persist
it SHALL surface as a run failure event (never a silent event-only
answer — the transcript is the truth).

#### Scenario: The answer lands after a failed tool call

- **WHEN** a run's last tool call fails and the model then produces
  its final answer
- **THEN** the transcript carries the final assistant row (not only
  a timeline event)
