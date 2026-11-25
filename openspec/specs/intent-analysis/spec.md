# intent-analysis Specification

## Purpose

Pre-execution intent analysis for goal runs: classify the task,
recommend resources from the semantic index, gate the approach, set
the step budget — cheap, typed, observable, and non-fatal.

## Requirements

### Requirement: A router bypasses analysis for trivial work

Before any intent-model call, a deterministic router SHALL bypass
analysis for greetings and non-task messages (the run proceeds
directly; a bypass event records the decision). No other message
reaches the model without analysis on root runs.

#### Scenario: Greeting bypasses
- **WHEN** a goal run's text is a greeting or smalltalk
- **THEN** no intent-model call is made and the run starts with a bypass event

### Requirement: The intent agent writes a typed contract

Root goal runs SHALL be analyzed by one small-model call producing a
typed contract: primary_intent, hidden_intents (actionable unstated
expectations, not labels), solution_path (high-level steps),
complexity (S/M/L/XL), recommended_skills/agents/mcp_tools (names from
the indexed resources only — names not in the index are dropped with a
warning event), ward_recommendation {action: use_existing|create_new,
name, archetype, reason} (use_existing requires an exact ward-index
match; names are reusable domain categories, never task-specific), and
execution_strategy {approach: simple|graph with concrete gating
criteria}. The analysis itself uses no tools.

#### Scenario: Full contract on a research task
- **WHEN** "Analyze my portfolio's tech holdings and write a report" starts a goal run
- **THEN** the analysis carries a kebab-case intent, a graph approach, ward recommendation, and indexed resource names only

#### Scenario: Hallucinated names are dropped
- **WHEN** the intent model recommends a skill not in the index
- **THEN** the recommendation is dropped and a warning event names the dropped item

### Requirement: Complexity sets the step budget

The contract's complexity SHALL map to the run's engine step budget
(S=4, M=8, L=16, XL=24) replacing the fixed default for that run.

#### Scenario: XL task gets the big budget
- **WHEN** analysis returns complexity XL
- **THEN** the run's bounded loop allows up to 24 steps

### Requirement: Analysis injects as a prompt section and gates approach

A successful analysis SHALL render as a `## Task Analysis` section in
the root run's system prompt (children unaffected), including the
fast-path instruction when approach=simple (skip multi-step
orchestration). The recommendations are advisory — the model decides.
The section SHALL additionally act on the ward recommendation:
`use_existing` instructs the root to call `ward_use(<name>)`;
`create_new` instructs `ward_create(<name>, <archetype>)`. The
analysis stays advisory — the MODEL calls the tools; the middleware
never enters a ward on its own.

#### Scenario: Simple approach stays lean
- **WHEN** analysis says approach=simple for a quick question
- **THEN** the injected section tells the model to answer directly without orchestration

#### Scenario: Recommendation becomes instruction
- **WHEN** the analysis recommends use_existing "stock-analysis"
- **THEN** the Task Analysis section names ward_use("stock-analysis")
  as the first structural step

### Requirement: Resources are discovered semantically

Skills, enabled MCP tools, and agents SHALL be embedded (name +
description) into a resource index via the local ONNX seam; the intent
agent's context carries the semantic recall hits for the user goal
(not the full listing) with a listing fallback for small rosters or a
cold cache. Registry changes (skill installed, MCP enabled, agent
edited) SHALL refresh the indexed entries.

#### Scenario: Semantic recall surfaces the right MCP
- **WHEN** the goal mentions "search the web" and a search MCP's tools are indexed
- **THEN** the recall hits include that MCP's tools without a full listing in the context

#### Scenario: Index refresh on registry change
- **WHEN** a new skill is installed while the daemon runs
- **THEN** the next analysis recalls it without a restart

### Requirement: Analysis is observable and non-fatal

Every decision SHALL emit events (started/complete/bypass/failed with
reason) and persist the result on the execution record. Analysis
failure NEVER blocks the run — it proceeds without the section and the
events say why.

#### Scenario: Model failure degrades gracefully
- **WHEN** the intent-model call fails
- **THEN** the run starts anyway, a failed event carries the reason, and no Task Analysis section is injected

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
