# Spec Delta

## ADDED Requirements

### Requirement: Child work is attributed on the wire

Every step a delegated child takes SHALL reach clients with the child's agent name and execution id, on the durable row and the live frame alike, and the transcript rows a child writes SHALL carry the same attribution — so a surface can show who did what without inferring it from order.

#### Scenario: The transcript names the author

- **WHEN** a delegated child writes its final report
- **THEN** that assistant row carries the child's agent name and execution id, distinct from the root's rows

#### Scenario: The stream names the author

- **WHEN** a child's tool call is streamed live and later replayed
- **THEN** both frames carry the child's agent name and execution id
