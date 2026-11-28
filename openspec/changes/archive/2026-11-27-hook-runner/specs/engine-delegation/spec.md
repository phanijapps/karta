# engine-delegation

## ADDED Requirements

### Requirement: SubagentStop is advisory on the fold-back
When a delegated child returns, the delegation SHALL raise SubagentStop
with the child's agent, status and execution in the payload before the
answer folds back. A block or warn SHALL append the hook's reason to the
fold-back so the root reads it next to the child's answer; an allow SHALL
leave the fold-back unchanged.

#### Scenario: The root reads the hook's reason beside the report
- **WHEN** a SubagentStop hook warns about a child's report
- **THEN** the fold-back carries the warning after the child's answer and the decision is on the Timeline attributed to the child
