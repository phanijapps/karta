# Proposal

## Why

Four recorded P2s have matured into friction: MCP tools run with no
per-call bounds or failure visibility (and the executor can't find
`npx`-style binaries outside the daemon's PATH — the brave-connect
failure every user hits), the OpenAI channel is non-streaming (chatty
clients wait on a whole response), sessions can't leave a ward once
entered (the binding is sticky with no exit), and research shells stay
"Research" forever (the design's auto-title never landed).

## What Changes

- **MCP hardening**: per-call timeout (60s → typed failure taxonomy
  in the Timeline: timeout / unavailable / protocol / server-error);
  binary resolution against the daemon PATH plus common tool dirs
  (node/pnpm/uv/cargo/go) so `npx`/`uvx` configs work as written; a
  bounded restart-backoff so a crash-looping server stops burning
  cycles; tool-call events for every MCP call (rule 4 — silent work is
  a bug).
- **/v1 streaming**: `stream: true` on `/v1/chat/completions` emits
  OpenAI chunked SSE (`chat.completion.chunk` frames, `[DONE]`
  terminator). Chat sessions stream real deltas; research sessions
  stream the live work (tool/delegation frames as chunk content with a
  final answer chunk) so OpenAI clients see the run working.
- **Leave ward**: `POST /api/sessions/{id}/leave-ward` clears the
  binding (session stays research, becomes a shell again); the
  composer's ward strip gains a Leave control; the engine host drops
  the ward on the next run.
- **Research auto-titling**: the first analysis titles its session
  from `primary_intent` (slugified, ≤ 64 chars) when the session has
  no title; the sidebar's Research group shows it; user titles win.

## Capabilities

### Modified Capabilities
- `ward-v2`: MCP hardening (bounds, taxonomy, binary resolution,
  events); leave-ward.
- `openai-channel`: streaming completions.
- `chat-modes`: research session auto-titling.

## Impact

`internal/mcpclient` (executor hardening), `internal/engine/eino`
(leave-ward host reset, MCP events), `internal/intent`+`store`+
`server` (titling, leave endpoint), `contracts/` (streaming shapes,
leave endpoint), frontend (Leave control, title rendering),
`cmd/zbotd` (binary path wiring).
