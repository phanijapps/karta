# Spec Delta

## Purpose

Two chat modes — Chat (fast, lean, personality-forward) and Research
(full pipeline) — on ONE surface with a mode-per-invoke wire field, a
Continue extension protocol, and the session-mode lifecycle.

## ADDED Requirements

### Requirement: Two session modes on one chat surface

The chat surface SHALL support exactly TWO execution modes selected
per invoke: `chat` (aliases: `fast`) and `research` (aliases:
`deep`, the default). The mode SHALL be a field on the chat-send
request (`mode`), persisted on the session, and changeable
mid-conversation.

- **Chat mode** (`chat`/`fast`): SKIP the intent-analysis pipeline,
  use the lean chat prompt, run with thinking disabled and higher
  temperature. Memory recall and skills still run — the mode gates
  PIPELINE DEPTH, not knowledge.
- **Research mode** (`research`/`deep`): the FULL pipeline — intent
  analysis, task analysis injection, ward recommendation and entry,
  planning, delegation. Full composed system prompt. Thinking and
  temperature from configuration.

#### Scenario: Chat mode answers directly

- **WHEN** a user sends a greeting in chat mode
- **THEN** the intent middleware is skipped entirely (no LLM analysis
  call), the lean chat prompt composes, and the reply arrives without
  a Task Analysis section

#### Scenario: Research mode on the same surface

- **WHEN** the same user's next message switches to research mode
- **THEN** the full pipeline runs: intent analysis, the composed
  system prompt with Task Analysis, ward tools offered, delegation
  available

#### Scenario: Mode persists on the session

- **WHEN** a session is created in chat mode and the user sends without
  specifying a mode
- **THEN** the session's last mode applies (chat)

#### Scenario: Memory runs in BOTH modes

- **WHEN** a chat-mode message references a prior fact
- **THEN** chitta recall injects the fact — the mode gates the
  pipeline, not the knowledge

### Requirement: Both modes carry the persona; research adds the pipeline

BOTH modes SHALL compose the persona sections (Role + Soul + OS +
Tools) — the agent's identity, voice, and principles never change
with mode. Chat mode carries the persona + Context (memory recall)
and nothing more. Research mode carries the persona + Context + the
dynamic sections (Task Analysis, Skills, Active Ward) + the research
protocol. The mode gates the PIPELINE (analysis, ward tools,
delegation), never the personality.

#### Scenario: Chat mode — lean but persona-complete

- **WHEN** a chat-mode exchange composes
- **THEN** the system prompt carries Role, Soul, OS, Tools, and a
  Context section (memory recall) — the SAME voice and identity as
  research mode — and does NOT carry research-protocol, Task
  Analysis, Skills, or Active Ward sections

#### Scenario: Research mode — the persona plus the pipeline

- **WHEN** a research-mode exchange composes
- **THEN** the system prompt carries Role, Soul, OS, Tools (the SAME
  persona as chat mode), the research protocol, AND the dynamic
  sections (Context, Task Analysis, Skills, Active Ward when
  hosting)

### Requirement: The Continue protocol extends live runs

A running session SHALL accept a `continue` control message granting
additional iterations (default 25). The progress tracker's extension
mechanism SHALL consume the grant: the hard budget extends by the
granted amount, the scoring window resets, and the run keeps going.
This is the user-side override on top of automatic extension.

#### Scenario: User extends a long research run

- **WHEN** a research run reaches its hard budget while productive
  and the user sends continue with additional_iterations=50
- **THEN** the run receives 50 more iterations and keeps working

### Requirement: Sessions carry a mode lifecycle

A session SHALL record its current mode (chat or research) as
first-class state. Mode changes SHALL take effect on the NEXT invoke,
not retroactively re-execute past turns. The session read API SHALL
expose the mode.

#### Scenario: Mode switch is next-invoke

- **WHEN** the user sends a research-mode message in a chat-mode
  session
- **THEN** that message runs with the full pipeline and the session's
  mode becomes research for subsequent messages

### Requirement: Both modes share the event stream and Timeline

Both modes SHALL emit to the same Timeline (agent steps, tool calls,
delegations, compactions). Chat-mode exchanges SHALL NOT emit
intent_analysis events (the pipeline was skipped); research-mode
exchanges SHALL emit them as today.

#### Scenario: Chat-mode events

- **WHEN** a chat-mode exchange completes
- **THEN** the Timeline carries the assistant step and any tool calls,
  with NO intent_analysis event
