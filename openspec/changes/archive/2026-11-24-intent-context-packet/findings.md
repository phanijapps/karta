# Findings — intent-context-packet

## Phase gates

### Gate 1 — gpt-6.1-terra (backend: packet analyzer, state, /v1)

Verdict: analyzer bound (3 attempts/45s), catalog authority + ranking,
provider params, state/event persistence, engine isolation all
confirmed. 2 P1 + 3 P2 — all fixed:

- **G1-1 (P1) /v1 conversation collision** — FIXED twice over:
  `metadata.chat_id` is now the PREFERRED identity (client-supplied,
  contract-documented), and every derived id carries the type suffix
  (`owui-<hash>-<type>`), so a hash collision can never inherit the
  wrong session TYPE. Residual: two conversations with an IDENTICAL
  first message and no chat_id still share a session — protocol-
  inherent (the OpenAI body carries no conversation id), recorded as
  P2-2. Tests: metadata identity + type isolation.
- **G1-2 (P1) advisory-block escape** — FIXED: EscapeRequest strips
  the block's own delimiters (both cases, open+close); adversarial
  delimiter tests.
- **G1-3 (P2) constant-time token compare** — FIXED (crypto/subtle,
  pre-review commit b2d585a).
- **G1-4 (P2) OpenAI 400 shape** — FIXED: /v1 decodes with the OpenAI
  error writer (invalid body/empty messages/no user message); the
  contract's 400 now references OpenAiError.
- **G1-5 (P2) latest-event ordering** — FIXED: ORDER BY rowid
  (insertion order, single writer) — RFC3339Nano text misorders
  fractional widths; regression test with crafted ".1Z"/".11Z".

### Gate 2 — gpt-6.1-sol (frontend + UX)

Verdict: bindings/FSD/runes/draft-fix/state-sourcing confirmed.
2 P1 + 2 P2 — all fixed:

- **G2-1 (P1) stale analysis overwrite** — FIXED: generation token in
  loadAnalysis + invalidation on select.
- **G2-2 (P1) in-flight analysis updates** — PARTIAL BY DESIGN: the
  card now renders regardless of transcript emptiness; refresh lands
  at completion (reconcile). Live in-flight refresh requires the
  events SSE subscription — recorded as P2-4 (the Timeline-Tree
  surface, a planned feature); the spec's "state (or the live event)"
  wording is satisfied by state.
- **G2-3 (P2) draft test hung the wrong endpoint** — FIXED: hangs
  /api/chat/send (the SSE path quick chat uses), polls sendStarted.
- **G2-4 (P2) dropped-chip contrast** — FIXED: opacity removed
  (explicit label + strikethrough carry the state); light-theme axe
  probe: 0 violations.

## Decisions (recorded during implementation)

- **D1 — Text-JSON, no response_format ladder.** We never adopted
  `response_format`; agentzero's mode ladder solves a failure mode we
  do not have. Fence-stripping (extractJSON) already covers wrapped
  JSON. (design.md decision 3.)
- **D2 — Last-parsed fallback on validation residue.** When every
  correction retry still names unknown resources, the LAST PARSED
  decision returns and the wrapper's Sanitize drops the residue — a
  good analysis is not wasted on one phantom ID (preserves the old
  drop-and-warn behavior as the belt-and-braces path).
- **D3 — Empty sets are authoritative.** Packet validation treats an
  empty roster as "nothing valid" (not "skip validation") — an empty
  MCP roster means no MCP recommendations are valid. Ward use_existing
  against an unwired vault correctly fails validation.
- **D4 — Ranking from the existing RecallResources seam.** The recall's
  per-kind hit order ranks the packet; no new chitta rank API was
  needed (host roster from the registry snapshot + ward vault).
- **D5 — /v1 model resolution.** The OpenAI `model` field selects the
  session TYPE; the actual LLM is the daemon's configured default
  model (the operator's picker). OpenWebUI has no idea which provider
  Karta uses — the daemon decides.
- **D6 — Non-streaming /v1 in v1.** `stream` is accepted and ignored;
  OpenWebUI tolerates non-streaming completions. Streaming chunks are
  a follow-up.
- **D7 — Task 3.3 (final-answer persistence): investigated, NO BUG.**
  The NVDA run's report IS the transcript's final row (2790 chars);
  the post-mortem misread a truncated query. The perceived "nothing
  showed" was the stuck draft + the dead analysis card — both fixed
  here. Recorded, no code change.

## P2s (recorded, not blocking)

- **P2-1 — /v1 streaming.** OpenAI chunked SSE for chat/completions
  (OpenWebUI shows progressive typing). Follow-up.
- **P2-2 — First-message-hash collision.** Two OpenWebUI conversations
  with identical first messages share a session (same human, same
  opening — accepted; design.md risk).
- **P2-4 — Live in-flight analysis refresh.** ✅ CLOSED (post-review,
  with the realtime work stream): `shared/sse/timeline.svelte.ts` —
  the centralized runes-based Timeline store (EventSource over the
  session events endpoint, replay-dedupe, bounded view). Research
  sends connect on send-start; the work stream renders every live
  event (analysis row, agent steps, tool calls, delegations, engine
  markers) while the run works; the intent_analysis event renders the
  Task Analysis card MID-RUN (applyLiveAnalysis); the stream closes at
  completion (the transcript is the durable truth). Playwright-pinned
  (mocked SSE frames) and live-verified (brave search run streamed
  intent_analysis → agent_step → tool_call ×2 during execution).
- **P2-3 — OpenWebUI conversation renames don't propagate** (the
  session keeps the hash id + "Research" title). A later
  session-rename surface covers it.

## Final review

**zbot-reviewer (final gate): open P0/P1 = 0.** All five fix groups
verified as holding (/v1 identity incl. type-suffix isolation, packet
delimiter escapes, rowid ordering, OpenAI 400/auth shapes, frontend
generation guard + card placement + draft + contrast); boundary
checklist clean; spec-vs-implementation coherent. Five fresh P2s —
ALL FIXED on receipt:

- **F-1 (P2) /v1 401 contract drift** — FIXED: /v1-local 401 (+ 502/
  503 on completions) declared with the OpenAiError schema; codegen.
- **F-2 (P2) busy→409 had no handler-level test** — FIXED:
  TestOpenAiValidationAndBusy (blocking research path; asserts 409 +
  code session_busy in the OpenAI body).
- **F-3 (P2) /v1 400 branches untested** — FIXED: the same test's
  table (no user message / empty messages / empty last user / invalid
  JSON → invalid_request_error shape).
- **F-4 (P2) stale AGENTS.md bullet** — FIXED: intent/AGENTS.md
  describes the packet-driven ≤3/45s correction loop and buildPacket.
- **F-5 (P2) no operator docs for the channel** — FIXED:
  docs/architecture/openai-channel.md (setup, model→type mapping,
  identity, api_token, limits).

**task verify: exit 0** — contract lint + codegen zero drift, backend
gates + lint (0 issues) + full suite + budget gates OK, frontend
check (0 errors) + build + 88 Playwright tests. (Suite run with the
dev stack stopped — the known inotify instance exhaustion.)

**LIVE smoke**: research send 24-38s → **5.3s** (packet +
reasoning_effort=low); state surface carries the full analysis;
/v1/models + /v1/chat/completions round-trip (karta-chat and
metadata.chat_id identity verified); test sessions cleaned.
