# engine-delegation Specification

## Purpose

Sub-agent delegation inside goal runs: Claude-style frontmatter definitions,
per-agent tool inventory, bounded depth grants, observable child runs, and
results folded back as tool outcomes.

## Requirements

### Requirement: Agents are defined in frontmatter markdown

Agent definitions SHALL be single markdown files under the config tree's
agents directory: YAML frontmatter (name, description, optional qualified
model, optional tools allowlist, optional max_depth) with the body as the
system prompt. Decoding SHALL be strict (unknown fields are load errors).
An agent without a description SHALL load but SHALL NOT be offered for
delegation. Existing YAML specs SHALL keep loading unchanged.

#### Scenario: Frontmatter agent loads
- **WHEN** the registry loads an agents directory containing a markdown file with valid frontmatter and a body
- **THEN** the snapshot carries the agent with its system prompt, model override, tool allowlist, and max_depth

#### Scenario: Unknown frontmatter field is a load error
- **WHEN** an agent file carries a field the spec does not define
- **THEN** the load reports a typed error naming the file and field, and the snapshot excludes it

### Requirement: Agent-named runs use the agent's wiring

A goal run whose request names a configured agent SHALL resolve that agent
and run with its system prompt, its model override when present, and its
tool allowlist; an unknown name SHALL fail typed before any model call; an
empty name keeps the default wiring. This closes the agent half of the
pinned unwired-fields finding.

#### Scenario: Named agent run
- **WHEN** a goal run names a configured agent with a model override
- **THEN** the run uses the agent's system prompt and qualified model, and the Timeline records the agent on its events

#### Scenario: Unknown agent fails typed
- **WHEN** a goal run names an agent that is not configured
- **THEN** the run fails with a typed error listing configured agents, before any provider call

### Requirement: Delegation is a depth-granted tool

A run SHALL be offered a `delegate_agent` tool only when its depth grant
allows further delegation (agent max_depth, bounded by a config hard cap).
The tool's description SHALL list delegatable agents with their
descriptions. Invoking it with an unknown agent SHALL return a typed tool
error the model can see. A successful delegation SHALL start a child run
under the named agent (depth+1, the child's own inventory, no delegate tool
unless its grant allows), SHALL emit the child's lifecycle as delegation
events into the parent's stream, and SHALL return the child's final answer
as the tool result. Cancelling the parent SHALL cancel children.

#### Scenario: Delegate and fold back
- **WHEN** a granted run delegates a task to a configured agent that completes
- **THEN** delegation events for the child's steps appear in the parent's stream and the tool result carries the child's final answer

#### Scenario: Depth refusal is invisible by construction
- **WHEN** a run's depth grant is exhausted
- **THEN** the delegate tool is absent from its offered tools (the model cannot attempt it)

#### Scenario: Unknown delegation target
- **WHEN** the model invokes delegate_agent with an unconfigured name
- **THEN** the tool returns a typed error listing configured agents, and the parent run continues

#### Scenario: Cancellation propagates
- **WHEN** a parent run is cancelled while a child is mid-flight
- **THEN** the child's run ends cancelled and the parent's stream closes without hanging

#### Scenario: The depth cap holds nesting off regardless of grants
- **WHEN** an agent's `max_depth` is raised above 0 while `delegation.max_depth` is 1
- **THEN** a delegated child is still not offered the delegate tool; raising the cap to 2 is what allows nesting

### Requirement: System-role agents are never offered for delegation

Agents reserved for host-run steps (the intent analyst) SHALL be excluded
from the delegate tool's routing table regardless of their description,
and an invocation naming one SHALL return a typed not-delegatable tool
error. They remain in the roster for Settings and for goal requests.

#### Scenario: The analyst is absent from the routing table
- **WHEN** a granted run's delegate tool is described while `intent-analyst` is configured with a description
- **THEN** the routing table omits it

#### Scenario: Invoking the analyst fails typed
- **WHEN** the model invokes `delegate_agent` for `intent-analyst`
- **THEN** the tool returns the not-delegatable error and no child run starts

### Requirement: Child work is attributed on the wire

Every step a delegated child takes SHALL reach clients with the child's agent name and execution id, on the durable row and the live frame alike, and the transcript rows a child writes SHALL carry the same attribution — so a surface can show who did what without inferring it from order.

#### Scenario: The transcript names the author

- **WHEN** a delegated child writes its final report
- **THEN** that assistant row carries the child's agent name and execution id, distinct from the root's rows

#### Scenario: The stream names the author

- **WHEN** a child's tool call is streamed live and later replayed
- **THEN** both frames carry the child's agent name and execution id

### Requirement: Delegated runs share the root's tool plane

A delegated child SHALL be offered the same tool plane the root run has — builtins, memory tools, the ward's file/shell tools, and the live MCP tools — bounded only by the agent's own `tools:` allowlist. Which MCP tool to use is the model's judgement; the host does not narrow the plane for children.

#### Scenario: A research child can search
- **WHEN** the root has MCP search tools mounted and delegates a research task
- **THEN** the child is offered those tools and its web searches appear as its own attributed tool calls

### Requirement: A tool-free first answer is nudged once

When an agent declares `require_tool_use: true`, a run under that agent whose FIRST completion carries no tool calls SHALL receive exactly one bounded nudge — the answer is returned to the model with the instruction to use the tools the task requires and report only what it actually did, or state explicitly that it could not — before the next answer is accepted as final. The nudge SHALL be observable as an agent-step event. Runs without the declaration are unaffected.

#### Scenario: The narrated research report is caught
- **WHEN** a research agent with `require_tool_use: true` answers its delegated task with prose describing tool calls it never made
- **THEN** the engine nudges it once before accepting an answer, and the Timeline carries the nudge event

#### Scenario: A synthesis task is not penalized
- **WHEN** an agent without `require_tool_use` answers directly
- **THEN** the answer is accepted on the first step exactly as before

### Requirement: The fold-back carries deliverable receipts

When a run is hosted in a ward and a delegated task names output paths (relative paths or bare filenames with an alphabetic extension; URLs are not paths), the delegate tool's result SHALL append a host-verified line per path — present inside the ward or not, an existence check at fold-back rather than proof the child created it — so the root reads the truth next to the child's claims. Without a hosted ward no receipts are appended. A path that would escape the ward (traversal, or a symlink leading outside) reads as not on disk.

#### Scenario: Missing deliverables are visible to the root
- **WHEN** a child claims files it never wrote
- **THEN** the tool result lists each named path with ✗ not on disk, and the root can discard the report without spending steps on verification
