# Design

## Context

The current analyzer (add-intent-analysis, slice 2) is a single
prompt-slot call: the wrapper recalls resource LINES (chitta), renders
them into the user-owned prompt template, calls the model twice on
parse failure (blind retry), then Sanitizes recommendations against
the live index (drop-and-warn). agentzero iterated FIVE times on this
exact shape and their settled design (gateway/.../middleware/intent/
{agent,catalog,inject,router,validation}.rs + the 2026-09-08 lessons
doc) is the evidence base for this rework.

## The packet analyzer

```
pre-checks (deterministic — already exist, kept)
  router.Bypass(goal): trivial → bypass (event carries the reason)
packet build (HOST-authoritative — internal/intent)
  catalog := registry snapshot (skills/agents/mcps/wards:
             id + name + description, from the live index)
  rank   := chitta recall(goal) — RANKS entries, never authorizes
  packet := per-kind sort by rank, truncate 20/20/20/64
decision call (ONE, with correction feedback — ≤3 attempts)
  system := user-owned intent prompt (slots) + output contract
  user   := <intent-data>{request + packet, escaped}</intent-data>
            + on retry: "previous decision failed host validation:
            {code}. {fix}. Return a corrected decision."
  deadline: 45s context timeout
  provider params: z.ai + glm-5.3* → reasoning_effort=low
validate against the packet's ID sets
  unknown IDs → correction code (unknown_resource) → retry
  final pass: existing Sanitize stays as the belt-and-braces guard
```

Why text-JSON and no response_format ladder: we never adopted
`response_format` — the lesson's failure mode does not apply; fence
stripping already exists (`extractJSON`). The ladder is complexity
without a failure to fix (recorded decision).

## Session state

The analysis is already persisted as a `intent_analysis` timeline
event (wrapper Cfg.Persist: {text, intent_analysis}). Session state
READS the latest such event per session — no new table, no new write
path:

```
GET /api/sessions/{id}/state →
  { session: Session, intent_analysis: {
      status, reason?, primary_intent?, complexity?, step_budget?,
      approach?, ward_action?, ward_name?, recommended_skills?,
      recommended_agents?, dropped?, budget } }
```

Bounded by the event payload (16KB contract cap is ample). The UI
card and context panel fetch this once per active session and refresh
after each send (reconcile already re-lists).

## UI

- **Draft**: clear at send start (`draft = ''` immediately after
  capture) — the multi-minute research run must not hold the text.
- **Analysis card**: render from session state (and the live SSE
  intent_analysis event when the run is in-flight). No transcript-row
  parsing anywhere.
- **Context panel**: Provisioned Skills / Agents read session state;
  the transcript-derived delegation roster stays (it is about actual
  delegations, not recommendations).

## Final-answer persistence (bug fix)

Symptom: the NVDA run's final report exists only in the last
`agent_step` event; no `msg-final` row (transcript ends on a tool
row). Prime suspect: the adapter's final-answer Append swallows errors
(`_, _ = a.st.Append(...)`) and/or the progress-tracker's nudge path
forces a final model turn outside the append branch. Fix per finding:
surface append failures as run failure events AND ensure every final
model answer flows through the msg-final append. Regression test: a
run whose last step is a failed memory write still lands its final
row.

## OpenAI channel (OpenWebUI)

```
GET  /v1/models        → [{id: "karta-research"}, {id: "karta-chat"}]
POST /v1/chat/completions
  body: {model, messages:[{role, content}...]}
  session id  := "owui-" + sha1(first user message)[:16]  (stable)
  session type:= model contains "chat" → chat, else research
  reply       := SendIn(session, last user message) → last assistant
                 message content; error → OpenAI error shape
  shape: non-streaming JSON (stream:false v1; OpenWebUI tolerates it)
```

- Contract-first: both paths + schemas land in contracts/openapi.yaml
  (our own components matching OpenAI's shapes).
- Channel-agnostic: the handler lives in internal/server over the
  ChatService seam — no engine/UI imports, sessions identical to any
  other client's.
- Auth: `server.api_token` (config.yaml) → require `Authorization:
  Bearer <token>` on /v1 only when set; unset keeps loopback-only
  posture (the existing request guard covers /v1 like any API path).
- Research sessions from OpenWebUI get the same busy/409 semantics;
  the OpenAI error body carries the message.

## Decisions

1. **Host metadata is authority; memory only ranks** (agentzero
   catalog.rs): the registry/index defines valid IDs; chitta recall
   orders the packet. Sanitize remains but should become a no-op in
   practice.
2. **Session state is read-from-events**, not a new store: one
   producer (the wrapper), one reader (the endpoint), no schema drift.
3. **No response_format ladder** — we are already in the reliable mode
   (text JSON + fence stripping); the ladder solves a failure we do
   not have.
4. **Correction feedback over blind retry** — the retry prompt names
   the failure code and the fix (agentzero's correction strings).
5. **OpenAI channel maps conversations, not messages**: one session
   per OpenWebUI conversation (first-message hash); history is
   OpenWebUI's concern (it resends it), the transcript is ours.
6. **Non-streaming v1** for /v1/chat/completions; streaming chunks
   are a follow-up when the operator asks.

## Risks / Trade-offs

- [Analysis still adds latency] — bounded at 45s worst case, low
  reasoning effort on z.ai cuts the common case; the pending bubble
  covers the wait.
- [First-message-hash collision] — two different OpenWebUI chats with
  identical first messages share a session; acceptable (same human,
  same opening) and recorded.
- [Packet prompt size] — caps bound it (~24 entries × ~600 chars +
  request echo at 32KB advisory cap, agentzero's numbers).

## Migration Plan

None: the analyzer's call shape is internal; the session-state and
/v1 endpoints are additive; the UI changes are client-side.

## Open Questions

- Should session state later carry the progress/budget counters
  (P2-1/P2-2 from add-chat-modes)? Left out — this change ships the
  analysis surface only.
