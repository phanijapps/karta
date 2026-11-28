# engine-delegation

## MODIFIED Requirements

### Requirement: Delegation is a depth-granted tool
A run SHALL be offered a delegation tool only when its depth grant allows
further delegation (agent max_depth, bounded by the config hard cap
`delegation.max_depth`, default 1 — the hold on nesting). On the ADK path
each delegatable registry agent SHALL be offered as a task tool (the child
runs as its own `ChatModelAgent` with isolated context; the child's final
answer is the tool result) — or, if the operator keeps the dispatch surface,
one `delegate_agent` tool whose description lists delegatable agents with
their descriptions. Invoking a delegation tool for an unknown or
non-delegatable agent SHALL return a typed tool error the model can see. A
successful delegation SHALL start a child run under the named agent
(depth+1, the child's own inventory, no delegation tool unless its grant
allows), SHALL emit the child's lifecycle as delegation events into the
parent's stream (child steps attributed to the child's execution), and
SHALL return the child's final answer as the tool result. Cancelling the
parent SHALL cancel children.

#### Scenario: Delegate and fold back
- **WHEN** a granted run delegates a task to a configured agent that completes
- **THEN** delegation events for the child's steps appear in the parent's stream and the tool result carries the child's final answer
#### Scenario: Depth refusal is invisible by construction
- **WHEN** a run's depth grant is exhausted
- **THEN** the delegate tool is absent from its offered tools (the model cannot attempt it)
#### Scenario: Unknown delegation target
- **WHEN** the model invokes a delegation tool with an unconfigured name
- **THEN** the tool returns a typed error listing configured agents, and the parent run continues
#### Scenario: Cancellation propagates
- **WHEN** a parent run is cancelled while a child is mid-flight
- **THEN** the child's run ends cancelled and the parent's stream closes without hanging
#### Scenario: The depth cap holds nesting off regardless of grants
- **WHEN** an agent's `max_depth` is raised above 0 while `delegation.max_depth` is 1
- **THEN** a delegated child is still not offered the delegate tool; raising the cap to 2 is what allows nesting

## ADDED Requirements

### Requirement: Child runs execute on the ADK spine
Delegated children SHALL run as ADK agents with the same middleware spine
as roots (tool plane, persistence, budget, context budgeting), their events
surfacing in the parent's stream through the child-event bridge with
child attribution, and their rows persisting under the child's execution
record exactly once.

#### Scenario: A child's steps are attributed once
- **WHEN** a delegated child performs model and tool steps
- **THEN** its rows persist under the child's execution, its steps surface in the parent's stream attributed to the child, and nothing is double-persisted
