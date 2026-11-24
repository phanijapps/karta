# Tasks

## 1. Packet analyzer

- [x] 1.1 `internal/intent`: packet builder — catalog from the
  registry/index (id + name + description per kind), chitta-recall
  ranking, truncation caps 20/20/20/64, request+packet embedded as
  escaped untrusted data. Verify: table tests (rank order, caps,
  escape, empty roster, recall failure degrades to alphabetical).
  — TestPacketRankedCapped + TestPacketValidate.
- [x] 1.2 Analyzer: correction-feedback retries (≤3 attempts, 45s
  deadline) — retry prompt carries the failure code + fix
  (invalid_json, unknown_resource…); validation against the packet's
  ID sets before Sanitize; last-PARSED fallback on residue (Sanitize
  drops the rest). Verify: fake completer scripts a bad-then-good
  sequence; unknown-ID recommendations trigger the correction path;
  deadline expires typed. — TestAnalyzerCorrectionFeedback,
  TestAnalyzerUnknownResidueFallsBackToParsed, TestAnalyzerDeadline.
- [x] 1.3 Provider params: completion extra-body hook (z.ai +
  glm-5.3* → `reasoning_effort: low`); providers.ChatClient accepts
  extra body params on the non-streaming path. Verify: request-body
  capture test asserts the param; other providers unaffected.

## 2. Session state + contract

- [x] 2.1 Store: latest intent_analysis event per session (read
  path). Verify: store test (two analyses — latest wins; sessions
  without one return none).
- [x] 2.2 Contract: `GET /api/sessions/{id}/state` (session +
  intent_analysis block carrying the provisioned skills/agents);
  codegen zero drift. Verify: handler test (state shape, 404 unknown).
  — TestSessionState; domain payload + wrapper carry recommended_*.

## 3. UI

- [x] 3.1 Draft clears at send start; composer stays usable during
  runs. Verify: Playwright (text gone immediately after Enter).
- [x] 3.2 Task Analysis card + context-panel skills render from
  session state — card renders regardless of transcript emptiness,
  refreshed at load/select/reconcile (completion time: research sends
  complete as units). Verify: Playwright with a stubbed state
  endpoint. — sessions.spec.ts: state-mocked card
  (intent/budget/skills/dropped), no card for chat, context-panel
  skills from state. Live in-flight SSE refresh = P2-4 (the events
  subscription is the Timeline-Tree surface).
- [x] 3.3 Final-answer persistence: INVESTIGATED — NO BUG. The NVDA
  run's final report IS the transcript's last row (seq 42, 2790
  chars; the earlier post-mortem misread a truncated query). The
  "nothing showed" perception was the stuck draft + the dead analysis
  card — both fixed by 3.1/3.2. Recorded; no code change.

## 4. OpenAI channel (OpenWebUI)

- [x] 4.1 Contract: `GET /v1/models` + `POST /v1/chat/completions`
  (OpenAI-shaped components, our contract); codegen. Verify: zero
  drift; lint clean.
- [x] 4.2 Handlers: session mapping (karta-research/karta-chat;
  owui-<sha1[:16]> id), reply assembly (last assistant message),
  OpenAI error shape, optional `server.api_token` bearer check.
  Verify: handler tests (chat + research model mapping, stable
  session id across turns, auth on/off, error mapping incl. busy).
  — TestOpenAiChannel (models/create/follow-up/auth); busy mapping
  typed via chat.ErrSessionBusy. LIVE: /v1/models + a karta-chat
  completion round-tripped (openwebui-link-ok).

## 5. Review and hardening

- [ ] 5.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol); fix P0/P1, P2s
  fix or record. Verify: ledger zero open P0/P1.
- [ ] 5.2 zbot-reviewer + `task verify` green + live smoke: NVDA
  re-run renders analysis card + final answer; OpenWebUI points at
  the daemon and chats. Verify: reviewer zero open P0/P1; verify
  exits zero; operator sees the OpenWebUI conversation in the
  sidebar.
