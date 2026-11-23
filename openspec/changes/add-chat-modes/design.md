# Design

## Context

Agentzero's gateway has two chat modes (verified in source):
- `SessionMode::Chat` (aliases `fast`, `chat`): skips the intent
  pipeline, lean prompt, thinking off, temperature 1.0, memory recall
  still runs, top_k=5, 900-token nudge ceiling.
- `SessionMode::Research` (aliases `deep`, `research`, default): the
  full pipeline — intent analysis, planning, delegation, wards. Full
  system prompt, thinking from config, temperature from config.
- The mode is per-invoke on the websocket `invoke` message
  (`mode: "deep" | "fast"`), persisted on the session, normalized by
  `normalized_invoke_mode` (research-* conversations force research).
- The Continue protocol: a websocket `Continue` message grants
  `additional_iterations` (default 25) to a running session.
- The lean prompt is loaded from `chat-instructions.md` (a DIFFERENT
  prompt file from the research system prompt).

## What Karta already has (and what maps)

| Agentzero piece | Karta equivalent |
|---|---|
| `SessionMode::Chat / Research` | **new** — a `mode` field on chat-send + the session |
| lean chat prompt (chat-instructions.md) | **already exists**: the composed static sections (Role + Soul + OS + Tools) ARE the lean prompt; research-protocol.md is the custom section that research-mode adds |
| intent pipeline skip in chat mode | **already exists**: the chat surface never runs the middleware; the wrapper is goal-run-only |
| memory in both modes | **already exists**: chitta recall in the chat Context section |
| ward tools in research mode only | **already mounted on chat** — the reframe: gate them by mode |
| Continue (additional_iterations) | **new**: wire into the progress tracker's Extend() |
| normalized_invoke_mode | session-mode normalization (research-* sessions force research) |
| thinking off + temp 1.0 in chat mode | **new**: the provider request adapts per mode |

## Decisions

1. **Mode on the request, not a separate endpoint**: chat-send gains
   `mode: "chat" | "research"` (default = the session's last mode,
   initial = `chat`). Aliases `fast`/`deep` accepted and normalized.
   No second endpoint — one surface, two depths.

2. **The lean prompt is the composed static sections MINUS the
   research protocol**: in chat mode, compose
   Role + Soul + OS + Tools + Context. Do NOT include
   research-protocol (the custom section), ward tools, Task Analysis,
   Skills, or Active Ward. In research mode, compose the full stack
   including research-protocol + ward tools. The research protocol's
   custom-section entry in config becomes a research-mode-only
   composition — the config keeps listing it; the chat-mode composer
   skips custom sections that are mode-gated (new: the section gains
   an optional `modes: [chat, research]` list; default = both).

3. **Thinking + temperature adapt per mode**: chat mode → thinking
   disabled, temperature 1.0 (agentzero's value). Research mode →
   from config. The chat service's Completer call carries these.

4. **Continue = the progress tracker's user-side extension**:
   `POST /api/sessions/{id}/continue` with
   `additional_iterations` (default 25). The engine's adapter holds a
   reference to the run's tracker; the continue endpoint grants the
   extension via a channel/closure. Automatic extension (productive
   runs) already works; Continue is the manual override.

5. **Session mode persistence**: the sessions table gains a `mode`
   column (default `chat`). Mode changes apply next-invoke. The
   session read API exposes it.

6. **No intent_analysis events in chat mode** — the pipeline was
   skipped; emitting a "bypassed" event for every chat message would
   be noise.

## Risks / Trade-offs

- [Mode thrash] — a user flipping modes per message pays the analysis
  cost on each research turn. Acceptable: the mode is a deliberate
  choice, not automatic.
- [Chat mode without ward tools] — a chat-mode user asking about wards
  gets a text-only answer. The mode switch IS the affordance.
- [Continue on a finished run] — the endpoint returns 409 if the run
  is not active; the client surfaces it.

## Migration Plan

Additive: the `mode` field defaults to `chat` (existing sends are
chat-mode — no behavior change since they already run lean). The
Continue endpoint is new. No data moves.

## Open Questions

- Should chat mode eventually get a MINIMAL ward read (see the ward
  index without entering)? Leaning no — the mode switch is cheap.
