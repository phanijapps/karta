# agent-engine Specification

## Purpose

The base engine layer: goal runs as store-backed executions behind the domain.AgentEngine seam, a pluggable tool-provider foundation for research/skills/MCP, registry-driven agent definitions, and durable event streaming.

## Requirements

### Requirement: Goal runs execute as store-backed sessions

`RunGoal` SHALL create (or reuse) a session and a root execution in the local store, append the goal as the opening user message, drive the run to a terminal state (completed or failed), and record every step as durable timeline events plus transcript rows in that execution. A failed model or tool step SHALL fail the execution with its error recorded, leaving committed rows intact.

#### Scenario: A goal run completes

- **WHEN** a goal runs against a reachable model
- **THEN** the execution reaches completed, the transcript holds the goal and the assistant's steps, and replay yields the events in order

#### Scenario: A goal run fails mid-step

- **WHEN** the model or a tool fails irrecoverably
- **THEN** the execution reaches failed with the error recorded, and committed rows and events remain

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

### Requirement: Tools arrive through a provider seam

The engine SHALL consume tools exclusively via `ToolProvider` (enumerate tool definitions; execute by name with JSON args). This change registers exactly the builtin provider (`clock.now`, `echo`); skills, MCP servers, and subagent tools register additional providers later without engine changes. Unknown tool requests execute as typed error results the model can see.

#### Scenario: A second provider needs no engine change

- **WHEN** a future change registers another ToolProvider
- **THEN** its tools are offered and executed through the same seam

#### Scenario: Unknown tool

- **WHEN** the model names a tool no provider offers
- **THEN** a typed error result is recorded and returned to the model

### Requirement: Agent definitions come from the registry

A goal MAY name a registry agent (`~/.zbot/agents`); its system prompt, optional model override, and tool allowlist SHALL shape the run. With no agent named and none configured, a minimal wiring prompt is used. Model precedence: the request's model, else the agent's, else the run is refused with guidance to select one. No agent is hardcoded in code.

#### Scenario: A configured agent shapes its run

- **WHEN** a goal names an agent with a system prompt and tool allowlist
- **THEN** the run uses that prompt, that model if set, and only allowlisted tools

#### Scenario: No model resolvable

- **WHEN** neither request nor agent names a model
- **THEN** the run is refused with guidance to select a model

### Requirement: Runs are cancellable

`Cancel` SHALL stop a session's active run at the next safe point, mark the execution cancelled with committed rows intact, and cease event emission for it. Cancelling an idle session is a no-op success.

#### Scenario: Cancel mid-run

- **WHEN** a run is cancelled while awaiting the model
- **THEN** the execution records cancelled, no further events emit, and committed rows remain

### Requirement: Runs stream durable-first timeline events

Every run step SHALL emit timeline events (agent steps, tool calls, session lifecycle) for the run's session, in order, each ATTEMPTED durable-first: the durable row is written before the live notification, and a failed write does not suppress the live event (the engine's recorded posture, pinned by its characterization tests and owned by the follow-up backend-engine-run-ownership). Every event — durable row and live frame alike — SHALL carry the acting agent ("root" or the sub-agent's name) and the execution it belongs to, so a replayed timeline is as attributed as the live one.

#### Scenario: Event order matches execution

- **WHEN** a run completes with a tool round
- **THEN** durable replay yields the lifecycle, step, and tool events in execution order

#### Scenario: Rows are attributed on both paths

- **WHEN** a delegated child makes a tool call
- **THEN** the durable row and the live frame both name the child's agent and execution, and a replay after restart shows the same attribution

#### Scenario: Legacy rows are attributed through their execution

- **WHEN** the stream replays a row persisted before agents rode the payload
- **THEN** its agent comes from the execution it belongs to (root or the delegated agent)

### Requirement: Delegation executes as child runs

The engine SHALL offer a delegation tool to runs whose depth grant allows it, and executing it SHALL run the named agent as a CHILD run of the same session: its own execution record under the parent's, its own bounded step budget, its agent's inventory, delegation events in the parent's stream, and its final answer folded back as the tool result. Each delegation SHALL emit a started event (agent, task, execution) when the child begins and a completed event (agent, execution, token usage, duration, terminal status) when it ends, so a delegation is a first-class Timeline node. The conformance suite SHALL carry delegation cases (fold-back, unknown target, depth refusal, cancellation propagation) every implementation must pass.

#### Scenario: Conformance covers delegation

- **WHEN** the shared conformance suite runs against an engine
- **THEN** delegate-and-fold-back, unknown-agent, depth-refusal, and cancellation-propagation cases all hold

#### Scenario: A delegation has a lifecycle with its cost

- **WHEN** a child run completes
- **THEN** the parent's stream carries `delegation_started` before the child's steps and `delegation_completed` after them, the latter with the child's execution id, summed tokens, duration, and status

### Requirement: Research-run assistant rows persist reasoning

The engine SHALL persist the model's reasoning text (`reasoning_content` / `reasoning`) on every assistant row it writes, exactly as the chat path does, so the transcript carries what the model thought for each step.

#### Scenario: Reasoning survives a research run

- **WHEN** a root run's final step comes from a reasoning model
- **THEN** the final assistant row carries that step's reasoning text, and replay after restart returns it intact

### Requirement: Assistant rows persist per-step usage

The engine SHALL write each completion's prompt and completion token usage on the assistant row it produces, for root and delegated runs alike, so a run's cost is attributable per step — not only per execution.

#### Scenario: A run's cost is readable per step
- **WHEN** a root run completes three model steps
- **THEN** each assistant row carries that step's prompt and completion tokens, and their sum equals the execution's totals

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
- **WHEN** a tool returns content over the configured byte cap on the ADK path
- **THEN** the model-facing history keeps a head-and-tail preview plus a pointer, the original bytes are retrievable by a ranged read of the offload, the durable rows keep the original content, and the delegate fold-back and loaded skill bodies are never capped
#### Scenario: Skills load on demand
- **WHEN** an ADK run starts with progressive injection and an analysis that recommended provisioned skills
- **THEN** the run's prompt carries the skills' names (not the bodies), the skill tool offers exactly those, and a body enters context only when the model loads it; an agent declaring `tools: []` is offered neither the skill tool nor the offload reader
#### Scenario: In-run compaction is the kernel policy
- **WHEN** an ADK run's context exceeds the compaction budget
- **THEN** the engine asks the `domain.Compactor` policy (never its own summarization logic), keeps the instruction and the structured recent tail verbatim, and emits a `compaction_triggered` event with before/after stats
#### Scenario: Children compact in-run
- **WHEN** a delegated child's context exceeds the summarization trigger
- **THEN** its history is compressed mid-run and the compaction is observable on the timeline
#### Scenario: Reasoning effort is per-agent
- **WHEN** an agent declares `reasoning_effort`
- **THEN** its model calls carry that effort (unset = the provider's default)
