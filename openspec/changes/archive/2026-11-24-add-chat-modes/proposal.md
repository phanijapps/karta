# Proposal

## Why

The mode-toggle model is wrong. The operator's insight: wards ARE
sessions. Each ward bot maintains its own persistent session (like
quick chat); research sessions start as shells and enter wards; the
sidebar IS the workspace navigator. No toggle — the session type IS
the mode. Mock UI:
docs/architecture/mock-session-model.html

## What Changes

- **Session types**: `chat` (one persistent quick-chat session, always
  exists), `research` (many, each its own conversation). A session's
  TYPE determines its pipeline depth — no mode field on sends.
- **Ward binding**: a research session can bind to a ward (sticky).
  The sidebar groups sessions: Chat, Ward Bots (ward-bound sessions),
  Research (shells without a ward yet). Each ward has ONE active
  session (its persistent bot).
- **Session CRUD**: list sessions (grouped by type + ward binding),
  create a research session ("+ New Research"), switch sessions (the
  chat surface renders the active session's transcript).
- **Session-scoped chat-send**: the send carries the session ID; the
  service routes by the session's type — chat sessions run lean,
  research sessions run the full pipeline (through the engine).
- **Ward sessions**: when a research session enters a ward (ward_use
  or ward_create), the binding sticks; subsequent messages in that
  session have the ward context pre-loaded (constitution, layout,
  canonical page). The session shows in the sidebar under the ward's
  name.
- **Continue protocol**: `POST /api/sessions/{id}/continue` grants
  additional iterations to a live research run.
- **Context panel**: the right sidebar shows the active session's
  context — ward card (canonical page, files), provisioned skills,
  agent roster with live status.
- **UI**: sidebar redesigned as the session navigator; composer adapts
  per session type; Task Analysis card + delegation tree + collapsed
  tool calls in research transcripts; ward context bar.

## Capabilities

### New Capabilities
- `chat-modes`: the session model — session types, ward binding,
  session CRUD, session-scoped routing, the Continue protocol, the
  context panel.

### Modified Capabilities
- `simple-chat`: chat-send becomes session-scoped (session_id); the
  service routes by session type.
- `api-contract`: session list/create endpoints, session-scoped
  chat-send, continue endpoint (additive).

## Impact

`internal/domain` (session type + ward binding), `internal/store`
(sessions table gains type + ward columns), `internal/server`
(session CRUD, session-scoped chat-send, continue), `internal/chat`
(session-scoped routing), `internal/engine/eino` (Continue wiring),
`contracts/` (codegen), frontend (sidebar + context panel +
session-aware Chat).
