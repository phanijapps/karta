# The OpenAI-Compatible Channel (OpenWebUI)

Karta's daemon speaks a small OpenAI-compatible surface so OpenWebUI
(or any OpenAI-shaped client) can chat with the full Karta pipeline —
wards, intent analysis, delegation — as the backend. Conversations
there become sessions here and appear in Karta's sidebar with the
complete analysis surface (Task Analysis card, context panel).

## Endpoints

- `GET  http://127.0.0.1:8787/v1/models` — advertises `karta-research`
  and `karta-chat`.
- `POST http://127.0.0.1:8787/v1/chat/completions` — the standard
  OpenAI body (`model`, `messages`). With `stream: true` the reply is
  chunked SSE: `chat.completion.chunk` frames (bare `data:` lines)
  ending in `finish_reason: stop` then `data: [DONE]`. Chat sessions
  stream real content deltas; research sessions stream the live work
  (🔧 tool calls, 👥 delegations, 🧠 steps) while the run executes, then
  the final answer.

## Model → session mapping

| Model           | Session type | Pipeline                                        |
| --------------- | ------------ | ----------------------------------------------- |
| `karta-research`| research     | intent analysis (context packet), wards, delegation, budgets |
| `karta-chat`    | chat         | the lean exchange (persona + memory recall)     |

The actual LLM is the daemon's configured default model (the header
picker's last-used selection) — the OpenAI `model` field only selects
the session type.

## Conversation identity

- **Preferred**: pass `metadata.chat_id` ("conv-…" — any stable id) in
  the request; it IS the session identity. Clients with real
  conversation ids never collide.
- **Fallback**: the sha1[:16] of the conversation's first user message
  (`owui-<hash>-<type>`). Two distinct conversations with an identical
  opening (and no chat_id) share a session — protocol-inherent; the
  type suffix keeps a collision from inheriting the wrong type.

Follow-up turns resend the whole history (the OpenAI protocol); Karta
sends only the last user message and appends to the same session.

## Authentication (optional)

Set `server.api_token: <secret>` in `~/.zbot/config/config.yaml` to
require `Authorization: Bearer <secret>` on `/v1`. Unset (default)
keeps the loopback posture. An operator exposing the daemon off-loopback
should always set it.

## OpenWebUI setup

1. Settings → Connections → add an OpenAI API connection:
   URL `http://127.0.0.1:8787/v1`, key anything (ignored unless
   api_token is set — then the exact token).
2. Pick `karta-research` for full-pipeline chats, `karta-chat` for
   quick questions.
3. Conversations appear in Karta's sidebar under Research (or the
   ward's bot once a ward is entered) — with the Task Analysis card
   and context panel.

## Limits (v1)

- Streaming research runs carry work frames as content (prefix
  markers) — OpenAI clients render only `delta.content`.
- A busy research session answers 409 `session_busy` as JSON before
  any stream opens (extend it with Karta's Continue, or wait). A failed
  run ends the stream with `finish_reason: error` and the message.
- OpenWebUI-side renames don't propagate to the session title.
