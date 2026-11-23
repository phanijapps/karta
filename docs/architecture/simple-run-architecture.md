# Simple Run Architecture (Chat)

Status: current behavior, plus the prompt composition that OpenSpec
change `add-intent-analysis` defines. The section "Current and planned
parts" tells you which is which.

Written in ASD Simplified Technical English (ASD-STE-100).

## Technical names

This document uses these technical names: agent, chitta, client,
daemon, daemon database, eventbus, Karta, memory, provider, session,
skill, SSE, system prompt, Timeline, tool, ward, zbot.db.

## Purpose

The simple run is a talk between one user and one agent. The user sends
a message. The agent sends one answer. The conversation has one
session. The session stays alive for the life of the daemon.

The simple run does not use intent analysis. It does not provision
skills. It does not use wards. It stays light and fast.

## Parts

| Part | Job |
|---|---|
| Client | The SPA, the desktop shell, or the CLI. It shows the conversation. |
| Daemon (`zbotd`) | It receives the message and controls the run. |
| Chat service | It builds the prompt, calls the provider, and stores the messages. |
| Provider plane | It sends the request to an OpenAI-compatible model. |
| System prompt | It tells the agent who it is. It is made of sections. |
| Tools | Built-in tools and chitta tools. The agent calls them when it needs data. |
| chitta | One LadybugDB. It holds memory records and the knowledge graph. |
| Daemon database (`zbot.db`) | It stores the session, the messages, and the Timeline. |
| eventbus | It takes each event to the Timeline and to all clients. |

## System prompt

The system prompt is composed from markdown files in the prompt
registry (`~/.zbot/config/prompts/`). The file `config.yaml` selects
them:

```yaml
system_prompt:
  role:   role      # required: identity and bounds
  soul:   soul      # optional: voice and principles
  os:     auto      # auto selects the file for the operating system
  tools:  tools     # optional: tool discipline
  custom: [house-rules]  # optional: more sections, in this order
```

The daemon puts the sections together in this order: Role, Soul, OS,
Tools, Custom. Then it adds the dynamic section `## Context`. This
section holds memory records that chitta recalls for the message.

The prompt has a strict rule: if a file is missing or has a bad name,
the daemon reports a load error. The daemon does not start a run with
an empty prompt by accident.

## Data flow

1. The client sends the message to `POST /api/chat/send`.
2. The chat service loads the session history from `zbot.db`.
3. The chat service composes the system prompt (static sections plus
   `## Context`).
4. The chat service sends the prompt and the history to the provider.
5. The provider returns the answer as a stream of tokens.
6. The chat service writes each message to `zbot.db`.
7. Each event goes to the eventbus. The eventbus persists the event to
   the Timeline and streams it to all clients through SSE.
8. The client shows the tokens as they arrive.

## Diagram

```
            +-------------------------------------------------+
            |                    CLIENT                       |
            |        (SPA / desktop shell / CLI)              |
            +------------------------+------------------------+
                                     |
                        POST /api/chat/send
                        SSE events (stream)
                                     |
            +------------------------v------------------------+
            |                  DAEMON (zbotd)                 |
            |                                                 |
            |  +----------------+      +-------------------+  |
            |  |  chat service  |----->|   system prompt   |  |
            |  |                |      |  ----------------  |  |
            |  |                |      |  Role  Soul  OS    |  |
            |  |                |      |  Tools  Custom     |  |
            |  |                |      |  ## Context (mem)  |  |
            |  |                |      +-------------------+  |
            |  |        |       |                             |
            |  |        v       |        tools                |
            |  |  +-----------+ |   +--------------------+     |
            |  |  | eventbus  |<----| builtin + chitta   |     |
            |  |  +-----+-----+     +---------+----------+     |
            |  |        |                     |                |
            |  +--------|---------------------|----------------+
            |           v                     v
            |  +------------+        +----------------+
            |  |  zbot.db   |        |     chitta     |
            |  |  sessions  |        | memory + graph |
            |  |  messages  |        |   (recall)     |
            |  |  Timeline  |        +----------------+
            |  +------------+
            +---------------------|---------------------------
                                  | OpenAI-compatible request
                                  v
                          +---------------+
                          |    PROVIDER   |
                          |    (model)    |
                          +---------------+
```

Solid lines show the message path. The eventbus gets an event for each
message, each tool call, and each error.

## Storage and memory

- The daemon database holds the session, the messages, and the
  Timeline. This is conversation data only.
- chitta holds memory records and the knowledge graph. The run recalls
  memory for `## Context`.
- The distiller reads closed sessions and writes durable facts into
  chitta memory. This work happens after the run.
- The simple run reads the knowledge graph only when the recall fusion
  includes it. Goal runs use the graph more. See the goal run document.

## What the simple run does not do

- It does not analyze intent before it answers.
- It does not inject skills into the context.
- It does not create or use wards.
- It does not delegate work to other agents. The chat loop has no
  delegate tool.

If the user asks for research work, the client sends that work to the
goal run. See `goal-run-architecture.md`.

## Rules that apply

1. All conversation state stays in `~/.zbot/`. No cloud storage, no
   remote state.
2. The daemon API matches `contracts/openapi.yaml`. Clients use the
   generated client only.
3. The core packages do not know the client code.
4. Every step emits an event. Silent work is a defect.

## Current and planned parts

| Part | Status |
|---|---|
| Chat service, session, streaming, Timeline, SSE | In the code |
| Tools (builtin + chitta) in chat | In the code |
| Composed system prompt (Role/Soul/OS/Tools/Custom) | Planned, change `add-intent-analysis` |
| `## Context` memory recall in chat | Planned, change `add-intent-analysis` |

## Related documents

- `goal-run-architecture.md` — the goal run (research mode)
- `architecture-brief.md` — the founding record
- `ward-research-exploration.md` — the ward concept and slices
- `../product/product-context.md` — product truth
