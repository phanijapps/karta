# Spec Delta

## MODIFIED Requirements

### Requirement: Reasoning output is captured

The daemon SHALL decode provider reasoning fields on both the streaming and non-streaming paths (`reasoning_content`, and the `reasoning` spelling), and SHALL persist the final reasoning text on the assistant message row — for chat exchanges AND for engine runs (root and delegated), whose assistant rows are transcript rows too. The message views (sync response, streaming terminal event, session reads) SHALL carry the reasoning when present.

#### Scenario: Reasoning survives restart
- **WHEN** a send from a reasoning model completes and the daemon restarts
- **THEN** replaying the session returns the assistant row with its reasoning text intact

#### Scenario: Synchronous sends keep reasoning too
- **WHEN** the user sends via the synchronous endpoint with a reasoning model
- **THEN** the response's assistant row carries the reasoning text

#### Scenario: Engine runs keep reasoning
- **WHEN** a research run's step comes from a reasoning model
- **THEN** the assistant row the engine writes carries that step's reasoning text
