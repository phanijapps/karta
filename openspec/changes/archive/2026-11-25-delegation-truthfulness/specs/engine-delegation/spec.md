# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
