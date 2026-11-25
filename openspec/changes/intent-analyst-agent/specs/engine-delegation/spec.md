# Spec Delta

## ADDED Requirements

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
