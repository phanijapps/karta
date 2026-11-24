# Proposal

## Why

The operator's NVDA deep-analysis run exposed the intent surface's
gaps end to end: analysis results never render (the wrapper persists
an EVENT only — the UI card parses message rows), the analysis call
takes 24–38s on zai/glm (no reasoning-effort control, no deadline),
the analyzer still uses the pre-fetch-and-sanitize anti-pattern that
agentzero's five-iteration rewrite explicitly retired (their lessons
doc, 2026-09-08), and the composer draft sits in the input for the
whole multi-minute run. Separately: the operator wants OpenWebUI as a
chat front-end to Karta (conversations there become sessions here,
with the same analysis surface).

agentzero's final design (gateway/gateway-execution/src/middleware/
intent/, studied in-repo) is the blueprint: a host-authoritative
CONTEXT PACKET (catalog + memory ranking, bounded), one decision call
with correction-feedback retries, validation against the packet's IDs,
and the analysis exposed as first-class SESSION STATE — not transcript
scraping.

## What Changes

- **Context packet** (the analyzer rewrite): the prompt carries a
  host-built catalog packet — full registry roster (skills, agents,
  MCPs, wards: id + name + description) RANKED by chitta recall,
  truncated (20/20/20/64) — with the user request embedded as
  untrusted data. Validation runs against the packet's ID sets; the
  drop-and-warn Sanitize remains only as the final guard.
- **Correction-feedback retries**: parse/validation failures re-prompt
  with the failure code + the fix instruction (≤3 attempts, 45s
  deadline) — no blind retries. Provider-aware completion params
  (z.ai glm-5.3* → `reasoning_effort: low`) to cut analysis latency.
- **Session state surface**: `GET /api/sessions/{id}/state` (contract
  first) returns the session row + the LATEST intent analysis
  (analysis, drops, failure reason, bypassed, budget) read from the
  persisted intent_analysis events. The analysis card, the context
  panel's skills/agents sections, and any external client read THIS —
  never transcript parsing.
- **UI fixes riding the same surface**: the composer draft clears at
  send start (not after the run); the Task Analysis card renders for
  engine-routed research runs (from session state + the live SSE
  event); the context panel's Provisioned Skills read from state.
- **Final-answer persistence fix**: the NVDA post-mortem found the
  root run's final report only in the timeline events — no message
  row. Investigate (swallowed Append error or a nudge-path gap) and
  fix: the transcript is the truth.
- **OpenAI-compatible channel (OpenWebUI link)**: `GET /v1/models` +
  `POST /v1/chat/completions` on the daemon (contract-first paths).
  Model `karta-research` / `karta-chat` selects the session type;
  conversation identity = stable hash of the first user message
  (OpenWebUI resends full history); the reply is the final assistant
  message. OpenWebUI conversations appear in Karta's sidebar as
  sessions with the full analysis surface. Non-streaming JSON in v1;
  optional bearer token (`server.api_token` in config.yaml) — absent
  token keeps the loopback posture.

## Capabilities

### New Capabilities
- `openai-channel`: the OpenAI-compatible endpoint pair mapping
  OpenAI-client conversations (OpenWebUI first) onto Karta sessions.

### Modified Capabilities
- `intent-analysis`: the analyzer becomes packet-driven with
  correction retries, deadline, and provider params; the analysis
  becomes queryable session state; the UI renders it.
- `api-contract`: session-state endpoint + the /v1 pair (additive).

## Impact

`internal/intent` (analyzer rewrite + retries), `internal/server`
(session-state handler, /v1 handlers), `internal/store` (latest
analysis event read), `contracts/` (codegen), `internal/providers`
(completion extra-body params — minimal), frontend (analysis card,
context panel, draft fix), `cmd/zbotd` (analysis wiring params).
