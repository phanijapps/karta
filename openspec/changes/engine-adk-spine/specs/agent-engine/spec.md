# agent-engine

## MODIFIED Requirements

### Requirement: The engine loop is bounded and tool-calling
The engine SHALL drive a tool-calling chat loop over the compaction-aware
history window for the run's execution scope, bounded by a maximum number of
steps per run. On the ADK path the loop SHALL be Eino's `ChatModelAgent`
ReAct loop under `adk.Runner` (final answer = a completion without tool
calls; the step bound = `MaxIterations`), with the engine's concerns
expressed as `ChatModelAgentMiddleware` implementations and tool wrappers.
Tools SHALL execute through the tool-provider seam only (adapted to the
framework's tool interface inside the engine adapter package); the engine
SHALL persist a tool-call intent before execution and a tool result row
after, emitting `tool_call` events. The loop terminates on a final assistant
answer, the step bound, or cancellation. ROOT runs MAY carry a pre-executed
intent analysis (prompt section + complexity-derived step budget replacing
the fixed bound); analysis absence, bypass, and failure are all valid run
states with no effect on the loop's semantics. The conformance suite SHALL
accept engines without analysis (the middleware is daemon-layer, not an
engine obligation).

#### Scenario: A run uses a tool
- **WHEN** the model requests a tool during a run
- **THEN** intent and result rows are persisted, a tool_call event is emitted, and the loop continues to a final answer
#### Scenario: The step bound stops runaway loops
- **WHEN** a run exceeds the maximum steps
- **THEN** it terminates with a recorded outcome rather than looping forever
#### Scenario: Budget override on analyzed runs
- **WHEN** a root run arrives with an analysis carrying complexity L
- **THEN** the loop's step bound is 16 for that run

## ADDED Requirements

### Requirement: Runs execute on the ADK spine behind the engine seam
The engine SHALL execute runs (root and delegated child) as
`adk.ChatModelAgent` runs under `adk.Runner`, consuming the runner's event
stream and translating it to durable-first `TimelineEvent`s (persist,
publish, stream — same posture and attribution as the legacy loop). Message
rows SHALL persist from middleware hooks (assistant rows after each model
call; tool rows from tool wrappers) under the run's execution record. Runs
SHALL remain revertible: the legacy hand-rolled loop SHALL stay selectable
by config for one soak cycle, and BOTH run paths SHALL pass the full
conformance suite.

#### Scenario: The engine seam is unchanged
- **WHEN** the run path switches between legacy and ADK
- **THEN** `domain.AgentEngine` (RunGoal/Resume/Cancel) behaves identically and the conformance suite is green on both paths
#### Scenario: Terminal states are described on the stream
- **WHEN** an ADK run fails, is cancelled, or hits the iteration bound
- **THEN** the failure/cancellation/step-bound is described as a streamed event before the channel closes and the execution row records the outcome

### Requirement: The first tool-free answer can be nudged once
A run whose agent declares `require_tool_use: true` that ends its FIRST
completion without tool calls SHALL receive one bounded nudge before the
answer is accepted: the answer row persists with empty tool calls, the
nudge rides a user message, and one continuation run (same execution
record) produces the accepted answer either way.

#### Scenario: The narrated research report is caught
- **WHEN** a `require_tool_use` child answers its task without any tool calls on its first completion
- **THEN** the answer is persisted with `ToolCalls "[]"`, one nudge runs, and the next answer is final
#### Scenario: A synthesis task is not penalized
- **WHEN** an agent without `require_tool_use` answers directly
- **THEN** the answer is accepted with no extra model call

### Requirement: The model context is budgeted
The engine SHALL bound what each model call re-sends. Tool results over the
configured cap SHALL be truncated in the model-facing history to a head
plus a pointer to the offloaded full content (retrievable by ranged read).
Skills SHALL be offered progressively on the ADK path (names and
descriptions in a tool; bodies load on demand) instead of injected as
prompt bodies. Roots AND children SHALL receive in-run summarization when
context exceeds the trigger threshold, and the compaction activity SHALL
be visible as timeline events. Agents MAY declare a reasoning effort the
engine passes to the model call.

#### Scenario: An oversized tool result is capped
- **WHEN** a tool returns content over the configured cap
- **THEN** the model-facing history keeps the head plus a pointer, the full content is retrievable by a ranged read, and the durable rows keep the original content
#### Scenario: Skills load on demand
- **WHEN** an engine run starts with skills provisioned
- **THEN** the run's prompts carry the skills' names and descriptions (not the bodies), and a skill body enters context only when the model loads it
#### Scenario: Children compact in-run
- **WHEN** a delegated child's context exceeds the summarization trigger
- **THEN** its history is compressed mid-run and the compaction is observable on the timeline
#### Scenario: Reasoning effort is per-agent
- **WHEN** an agent declares `reasoning_effort`
- **THEN** its model calls carry that effort (unset = the provider's default)
