# Proposal

## Why

Agentzero runs TWO chat modes on one surface — Chat (fast, lean,
personality) and Research (deep, full pipeline) — with the mode
per-invoke and a Continue protocol for live extension. Karta today
hard-splits: chat is a separate dumb surface, goal runs are the only
smart one. The operator's direction: the CHAT is the research surface
(ward tools already mounted); what's missing is the mode switch that
makes the same conversation lean OR deep, plus the Continue protocol
that lets a user extend a live run.

## What Changes

- **`mode` field on chat-send** (contract: `mode: "chat" | "research"`
  with aliases `fast`/`deep`, default = the session's last mode,
  initial default = `chat`). Persisted on the session.
- **Chat mode** skips the intent pipeline entirely; composes the LEAN
  prompt (Role + Soul + OS + Tools + Context — no research protocol,
  no ward tools); thinking off; temperature raised. Memory recall and
  builtin tools still run.
- **Research mode** on the chat surface = the full pipeline (currently
  goal-run-only): intent analysis, Task Analysis, ward tools,
  delegation, progress-aware budgets. This UNIFIES chat and goal runs
  into one surface with two depths.
- **Continue protocol**: `POST /api/sessions/{id}/continue` grants
  additional iterations to a live run (default 25); consumed by the
  progress tracker's extension mechanism.
- **Session mode lifecycle**: the session record carries its current
  mode; the session API exposes it; mode changes apply next-invoke.
- Contract: chat-send gains `mode`; sessions gain `mode`; the continue
  endpoint. Codegen.

## Capabilities

### New Capabilities
- `chat-modes`: the two-mode chat surface, lean-prompt composition,
  the Continue protocol, the session-mode lifecycle.

### Modified Capabilities
- `simple-chat`: the chat-send request gains `mode`; the chat service
  branches on it.
- `api-contract`: the mode field + continue endpoint (additive).

## Impact

`internal/chat` (mode branch: lean vs full prompt; ward tools
research-only), `internal/server` (chat-send mode param; the continue
endpoint), `internal/engine/eino` (Continue wiring into the progress
tracker), `internal/domain` (session mode type), `contracts/`
(codegen), `internal/config/seedassets/prompts/` (the lean chat
prompt's sections already exist — Role/Soul/OS/Tools ARE the lean
prompt; the research protocol rides as a custom section that
research-mode adds).

## Design notes

The key insight from agentzero: **memory runs in BOTH modes — the
mode gates pipeline depth, not knowledge.** Chat mode is not a dumb
endpoint; it's the same agent, lean prompt, no ceremony. Research mode
is the same surface with the full engine behind it.

The lean prompt is ALREADY what the composed system prompt's static
sections give us (Role + Soul + OS + Tools). The research protocol
(research-protocol.md) and the ward tools mount ONLY in research
mode. No new prompt files needed — just conditional composition.

The Continue protocol is the user-side extension on top of the
progress tracker's automatic extension: productive runs extend
themselves; the user can grant more.
