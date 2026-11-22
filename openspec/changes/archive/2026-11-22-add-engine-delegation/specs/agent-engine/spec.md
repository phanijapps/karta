# Spec Delta

## ADDED Requirements

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
