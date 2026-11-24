# chat-modes Specification

## Purpose

The session model — wards ARE sessions. Two session types (`chat`,
`research`) on ONE chat surface with NO mode toggle: a session's TYPE
determines its pipeline depth. Session CRUD, session-scoped chat-send,
sticky ward binding, the Continue protocol, and the context panel.
Mock UI: docs/architecture/mock-session-model.html.

## Requirements

### Requirement: Two session types, no mode toggle

The chat surface SHALL support exactly two session TYPES:
`chat` (ONE persistent quick-chat session, id `default`, always
exists) and `research` (many, each its own conversation). The type
SHALL be set at creation and IMMUTABLE afterwards; there SHALL be no
mode field on chat sends and no mode toggle in the UI — the active
session's type IS the mode.

- **Chat sessions** run the LEAN pipeline: no intent analysis, no ward
  tools, no delegation; memory recall and the persona still run.
- **Research sessions** run the FULL pipeline through the engine:
  intent analysis, skills provision, ward tools, delegation, progress
  budgets.

#### Scenario: Quick chat answers directly

- **WHEN** a user sends a greeting in the quick-chat session
- **THEN** the intent middleware is skipped entirely (no LLM analysis
  call), the lean chat prompt composes, and the reply arrives without
  a Task Analysis section

#### Scenario: A research session runs the full pipeline

- **WHEN** the user creates a research session and sends a message in
  it
- **THEN** the full pipeline runs: intent analysis, the composed
  system prompt with Task Analysis, ward tools offered, delegation
  available

#### Scenario: No mode field anywhere

- **WHEN** any client sends a chat message
- **THEN** the request carries `session_id` (not a mode); the service
  loads the session and routes by its type

#### Scenario: Memory runs in BOTH types

- **WHEN** a chat-session message references a prior fact
- **THEN** chitta recall injects the fact — the type gates the
  pipeline, not the knowledge


### Requirement: Both types carry the persona; research adds the pipeline

BOTH session types SHALL compose the persona sections (Role + Soul +
OS + Tools) — the agent's identity, voice, and principles never change
with type. Chat sessions carry the persona + Context (memory recall)
and nothing more. Research sessions carry the persona + Context + the
dynamic sections (Task Analysis, Skills, Active Ward) + the research
protocol.

#### Scenario: Chat session — lean but persona-complete

- **WHEN** a chat-session exchange composes
- **THEN** the system prompt carries Role, Soul, OS, Tools, and a
  Context section (memory recall) — the SAME voice and identity as
  research sessions — and does NOT carry research-protocol, Task
  Analysis, Skills, or Active Ward sections

#### Scenario: Research session — the persona plus the pipeline

- **WHEN** a research-session exchange composes
- **THEN** the system prompt carries the SAME persona plus the
  research protocol AND the dynamic sections (Context, Task Analysis,
  Skills, Active Ward when hosting)


### Requirement: Session CRUD

The daemon SHALL expose session list and create endpoints. GET
/api/sessions returns sessions with `type`, `ward` (when bound), and
recency ordering. POST /api/sessions creates a NEW research session
and returns it. The one chat session is NOT deletable; research
sessions exist until user-deletion lands (a later change).

#### Scenario: The sidebar renders from the session list

- **WHEN** the client loads the session list
- **THEN** the response carries every session with type and ward
  binding, and the sidebar groups them: Chat (the one default
  session), Ward Bots (research sessions WITH a ward binding), and
  Research (shells without a ward)

#### Scenario: New Research creates a shell

- **WHEN** the user clicks [+ New Research]
- **THEN** a research session (type=research, ward=nil) is created
  and becomes the active session with an empty transcript


### Requirement: Session-scoped chat-send

The chat-send request SHALL carry `session_id` (default `default`).
The service SHALL load the session and route by its TYPE: chat
sessions run the lean exchange in that session's transcript; research
sessions run the engine's full pipeline appended to the SAME
transcript. Sync and clear operate per session. An unknown session id
fails typed (404).

#### Scenario: Switching sessions switches the transcript

- **WHEN** the user switches the active session in the sidebar
- **THEN** the chat surface renders that session's transcript and
  the next send appends to it

#### Scenario: Unknown session

- **WHEN** a send names a session id that does not exist
- **THEN** the daemon responds 404 and nothing is appended


### Requirement: Sticky ward binding on research sessions

A research session SHALL record its active ward (the `ward` column).
When `ward_use` or `ward_create` fires inside a research session, the
binding updates (entering a different ward SWITCHES the binding).
Subsequent sends in that session pre-load the ward context
(constitution + layout packet) before the run; a binding whose ward no
 longer exists in the vault self-heals to unbound. The sidebar groups
 ward-bound sessions under the ward's name.

#### Scenario: Entering a ward sticks

- **WHEN** the model calls ward_use("stock-analysis") in a research
  session
- **THEN** the session's ward binding becomes stock-analysis, the
  sidebar re-groups the session under Ward Bots, and the next message
  in that session runs with the ward context pre-loaded

#### Scenario: Restart preserves the binding

- **WHEN** the daemon restarts and the user sends in a ward-bound
  research session
- **THEN** the ward host re-enters the bound ward from the session
  row before the run starts


### Requirement: The Continue protocol extends live runs

A running research session SHALL accept a `continue` control message
granting additional iterations (default 25). The progress tracker's
extension mechanism SHALL consume the grant: the hard budget extends
by the granted amount, the scoring window resets, and the run keeps
going. This is the user-side override on top of automatic extension.

#### Scenario: User extends a long research run

- **WHEN** a research run reaches its hard budget while productive
  and the user sends continue with additional_iterations=50
- **THEN** the run receives 50 more iterations and keeps working


### Requirement: The context panel surfaces the active session's context

The right panel SHALL render the ACTIVE session's context: the ward
card (canonical title, archetype, lint status, layout) when the
session is ward-bound, the provisioned skills, and the agent roster.
Chat sessions render no ward card.

#### Scenario: Ward-bound session context

- **WHEN** the active session is ward-bound
- **THEN** the context panel shows the ward card and the session's
  provisioned skills and agents

#### Scenario: Chat session context

- **WHEN** the active session is the quick chat
- **THEN** the context panel shows no ward card


### Requirement: Both types share the event stream and Timeline

Both types SHALL emit to the same Timeline (agent steps, tool calls,
delegations, compactions). Chat exchanges SHALL NOT emit
intent_analysis events (the pipeline was skipped); research exchanges
SHALL emit them as today.

#### Scenario: Chat-session events

- **WHEN** a chat-session exchange completes
- **THEN** the Timeline carries the assistant step and any tool calls,
  with NO intent_analysis event
