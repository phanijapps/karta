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

Every analysis SHALL emit a full event trail — `analysis_started`
(model, output mode, resolved window and its source) then exactly one
terminal event: `analysis_completed` (the contract summary plus tokens
in, tokens out, duration ms, attempts, and whether the safety valve
fired), `analysis_bypassed` (reason), or `analysis_failed` (reason,
attempts). Each analyzed run SHALL record a dedicated execution row for
the intent-analyst agent under the root run, carrying its token usage.
Analysis failure NEVER blocks the run — it proceeds without the section
and the events say why.

#### Scenario: Model failure degrades gracefully
- **WHEN** the intent-model call fails
- **THEN** the run starts anyway, a failed event carries the reason, and no Task Analysis section is injected

#### Scenario: The trail has a start and a terminal event
- **WHEN** a root research run is analyzed
- **THEN** the Timeline carries `analysis_started` followed by exactly one of `analysis_completed`, `analysis_bypassed`, or `analysis_failed`, in that order, before the run's own steps

#### Scenario: Tokens are accounted on a dedicated execution row
- **WHEN** an analysis completes after two attempts
- **THEN** the intent-analyst's execution row for that run is terminal with the summed prompt and completion tokens of both attempts, and `analysis_completed` carries the same totals, the duration, and `attempts: 2`

#### Scenario: Trivial and disabled runs bypass with a reason
- **WHEN** the router bypasses the goal or analysis is disabled by config
- **THEN** `analysis_bypassed` carries the reason and no analysis execution row is created

#### Scenario: Legacy rows still replay
- **WHEN** the event stream replays a pre-existing `intent_analysis` row
- **THEN** it is emitted as the terminal `analysis_*` kind matching its recorded status, with its payload intact

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

The daemon SHALL expose the active session's LATEST terminal analysis
(`analysis_completed`, `analysis_bypassed`, or `analysis_failed`) as
session state (`GET /api/sessions/{id}/state`): the session row plus the
analysis block — the FULL contract the analyst decided (primary intent,
hidden intents, solution path, complexity, budget, approach and its
explanation, ward action/name/archetype/reason, recommended
skills/agents/MCP tools, dropped names) and, when analyzed, model,
tokens in/out, duration, attempts. The chat surface's Task Analysis
card and the context panel's Provisioned Skills section SHALL render
from this state (or the live event) — never by parsing transcript rows.

#### Scenario: The card renders for engine-routed research runs
- **WHEN** a research session's state carries an analyzed intent
- **THEN** the chat surface renders the Task Analysis card with the
  intent, complexity, approach, ward, provisioned skills, and dropped
  names

#### Scenario: The analyst's full decision reaches the surface
- **WHEN** an analysis decides hidden intents, a solution path, MCP
  recommendations, a ward reason, and an explanation
- **THEN** the completed event, the session state, and the Task Analysis
  popover carry every one of them

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

### Requirement: The analyst is a registered system agent

The intent-analyst SHALL be a registry agent (`intent-analyst`: no
tools, `max_depth: 0`, seeded write-once into `~/.zbot/agents/` with its
rubric referenced by prompt name) — listed in Settings → Agents, with
its prompt body and model editable like any agent. It is a host-run
decision step: it SHALL never be offered for delegation and SHALL never
appear in its own catalog packet. Its decision remains a validated single
completion with bounded correction retries, never a tool loop.

#### Scenario: The analyst appears in the roster
- **WHEN** Settings → Agents lists the roster on a seeded home
- **THEN** `intent-analyst` is listed with no tools and no delegation grant

#### Scenario: Delegation refuses the analyst
- **WHEN** a run's delegate tool is described or invoked for `intent-analyst`
- **THEN** it is absent from the routing table and an invocation returns a typed not-delegatable error

#### Scenario: An inlined rubric wins over the prompt reference
- **WHEN** the agent file's body carries every resource slot marker
- **THEN** that body is the rubric; otherwise the referenced `intent-analysis` prompt is, with the embedded default as the last fallback

### Requirement: Token limits derive from the selected model

The analyst's limits SHALL be the selected model's own: the context
window resolves from the catalog entry's discovered window, else a
model-id prefix in the user-editable `model_limits.yaml` (built-in table
first), else 32,000. The completion SHALL send `max_tokens` equal to the
window. The composed prompt SHALL be measured; per-kind packet caps
shrink ONLY when it would exceed `window − 4,000`. No token knob is
exposed.

#### Scenario: The window follows the resolution chain
- **WHEN** the analysis model has a window discovered from Ollama's `/api/show` (`*.context_length`), another has only a `model_limits.yaml` entry, and a third is unknown
- **THEN** their windows are the discovered value (source `ollama`), the table value (source `table`), and 32,000 (source `fallback`)

#### Scenario: The analyst asks for the full window
- **WHEN** an analysis call is made for a model with a 128,000 window
- **THEN** the request carries `max_tokens: 128000` and every packet cap stays at its default

#### Scenario: The safety valve fires only on a tiny window
- **WHEN** the composed prompt would exceed `window − 4,000`
- **THEN** the packet's per-kind caps shrink proportionally until it fits, the attempt proceeds, and the completed event reports that trimming fired

#### Scenario: A provider rejecting the full window still analyzes
- **WHEN** the provider rejects the request because of `max_tokens`
- **THEN** the same attempt is retried once without `max_tokens` before any failure is recorded

### Requirement: The analyst's model follows one precedence chain

The analysis model SHALL resolve in exactly this order: the session's
bound ward's orchestrator model when a ward orchestrates; otherwise the
Settings card selection, the agent spec's `model`, `intent_analysis.model`
from config.yaml, then the run's model. The resolved model SHALL be
reported on every analysis event.

#### Scenario: A ward orchestrator wins
- **WHEN** the session is bound to a ward whose orchestrator model is set and the Settings card names another model
- **THEN** the analysis runs on the ward's orchestrator model and the events name it

#### Scenario: The card wins outside a ward
- **WHEN** no ward orchestrates and the Settings card names a model
- **THEN** the analysis runs on that model regardless of the agent spec or config.yaml
