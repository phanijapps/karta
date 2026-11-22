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

The engine SHALL drive an OpenAI-compatible chat loop with tool calling over the compaction-aware history window for the run's execution scope, bounded by a maximum number of steps per run. Tools SHALL execute through the tool-provider seam only; the engine SHALL persist a tool-call intent before execution and a tool result row after, emitting `tool_call` events. The loop terminates on a final assistant answer, the step bound, or cancellation.

#### Scenario: A run uses a tool

- **WHEN** the model requests a tool during a run
- **THEN** intent and result rows are persisted, a tool_call event is emitted, and the loop continues to a final answer

#### Scenario: The step bound stops runaway loops

- **WHEN** a run exceeds the maximum steps
- **THEN** it terminates with a recorded outcome rather than looping forever

### Requirement: Tools arrive through a provider seam

The engine SHALL consume tools exclusively via `ToolProvider` (enumerate tool definitions; execute by name with JSON args). This change registers exactly the builtin provider (`clock.now`, `echo`); skills, MCP servers, and subagent tools register additional providers later without engine changes. Unknown tool requests execute as typed error results the model can see.

#### Scenario: A second provider needs no engine change

- **WHEN** a future change registers another ToolProvider
- **THEN** its tools are offered and executed through the same seam

#### Scenario: Unknown tool

- **WHEN** the model names a tool no provider offers
- **THEN** a typed error result is recorded and returned to the model

### Requirement: Agent definitions come from the registry

A goal MAY name a registry agent (`config/agents`); its system prompt, optional model override, and tool allowlist SHALL shape the run. With no agent named and none configured, a minimal wiring prompt is used. Model precedence: the request's model, else the agent's, else the run is refused with guidance to select one. No agent is hardcoded in code.

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

Every run step SHALL emit timeline events (agent steps, tool calls, session lifecycle) that are persisted durably before live notification, in order, for the run's session — the same durable-first contract as chat.

#### Scenario: Event order matches execution

- **WHEN** a run completes with a tool round
- **THEN** durable replay yields the lifecycle, step, and tool events in execution order

### Requirement: Delegation executes as child runs

The engine SHALL offer a delegation tool to runs whose depth grant allows
it, and executing it SHALL run the named agent as a CHILD run of the same
session: its own execution record under the parent's, its own bounded step
budget, its agent's inventory, delegation events in the parent's stream,
and its final answer folded back as the tool result. The conformance suite
SHALL carry delegation cases (fold-back, unknown target, depth refusal,
cancellation propagation) every implementation must pass.

#### Scenario: Conformance covers delegation

- **WHEN** the shared conformance suite runs against an engine
- **THEN** delegate-and-fold-back, unknown-agent, depth-refusal, and cancellation-propagation cases all hold
