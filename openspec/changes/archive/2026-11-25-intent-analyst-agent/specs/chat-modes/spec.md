# Spec Delta

## MODIFIED Requirements

### Requirement: Both types share the event stream and Timeline

Both types SHALL emit to the same Timeline (agent steps, tool calls,
delegations, compactions). Chat exchanges SHALL NOT emit analysis events
(the pipeline was skipped); research exchanges SHALL emit the
`analysis_*` event kinds (started plus one terminal completed / bypassed
/ failed) as today.

#### Scenario: Chat-session events
- **WHEN** a chat-session exchange completes
- **THEN** the Timeline carries the assistant step and any tool calls,
  with NO analysis event
