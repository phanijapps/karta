# Spec Delta

## MODIFIED Requirements

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

### Requirement: The analysis is queryable session state

The daemon SHALL expose the active session's LATEST terminal analysis
(`analysis_completed`, `analysis_bypassed`, or `analysis_failed`) as
session state (`GET /api/sessions/{id}/state`): the session row plus the
analysis block (status, reason, primary intent, complexity, budget,
approach, ward action/name, recommended skills/agents, dropped names, and
— when analyzed — model, tokens in/out, duration, attempts). The chat
surface's Task Analysis card and the context panel's Provisioned Skills
section SHALL render from this state (or the live event) — never by
parsing transcript rows.

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

## ADDED Requirements

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
