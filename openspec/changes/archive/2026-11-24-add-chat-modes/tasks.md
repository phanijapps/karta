# Tasks

## 1. Domain + store + contract

- [x] 1.1 `domain.SessionType` (`chat` | `research`); sessions store
  gains `type` (rename `mode`) + `ward` columns; migration. Verify:
  store round-trip (create with type, ward binding set/unset, list
  grouped). — TestSessionModelRoundTrip + TestMigrateSessionType
  (toggle-era mode column renames to type; ward added; idempotent).
- [x] 1.2 Contract: GET /api/sessions returns sessions with `type` +
  `ward`; POST /api/sessions creates a research session (returns the
  new session); POST /api/sessions/{id}/continue (unchanged).
  Codegen. Verify: zero drift; session list shape tested. — Also:
  ChatSend mode → session_id; sync/clear gained session_id;
  IntentAnalysisPayload gained recommended_skills/agents (additive);
  DELETE /api/chat/messages joined the contract. TestSessionCRUD.

## 2. Session-scoped routing

- [x] 2.1 Chat service: the send carries `session_id`; the service
  loads the session (type + ward), routes chat sessions lean /
  research sessions through the engine's full pipeline. The ward
  binding pre-loads the ward context (constitution + layout via the
  ward host). Verify: table tests — chat session routes lean,
  research session routes full, ward-bound session has ward context.
  — TestSessionRouting (lean/research/fallback/typed-404/unit-
  completion); TestHostedRootRunComposesActiveWard (## Active Ward
  composes for hosted roots; EnsureWard restart recovery; binder
  fires once); TestOnEnterFires (wardtools hook). Daemon wiring:
  cmd/zbotd/researchpath.go (wireSessionModel + wireLeanChat).
- [x] 2.2 Server: chat-send reads `session_id`; session CRUD
  endpoints (list with type/ward grouping, create research); ward
  binding updates when ward_use/ward_create fires in a session.
  Verify: handler tests (list grouped, create, ward binding update).
  — TestSessionCRUD (create/list/get/ward binding/typed 404s). The
  binding update: engine SetWardBinder → store.SetSessionWard via
  wardtools Host.SetOnEnter. Verified live: a research send created a
  ward and the binding stuck.

## 3. UI — the session navigator

- [x] 3.1 Sidebar redesigned: session list grouped by type (Chat /
  Ward Bots / Research); active session highlighted; [+ New Research]
  button; ward sessions show ward icon + name; shells show research
  icon. Verify: Playwright (session list renders, groups correct,
  switching works, new research creates a session). —
  tests/sessions.spec.ts (groups + switching + new research).
- [x] 3.2 Context panel (right sidebar): active ward card (canonical
  page excerpt, file list), provisioned skills, agent roster with
  live status. Verify: Playwright (context renders for ward-bound
  session, absent for chat). — tests/sessions.spec.ts (ward card +
  canonical title + layout tree + skills + agents; no ward card for
  chat). Live status is derived from persisted delegations (done);
  a live-run indicator needs run-state surfacing (P2, recorded).
- [x] 3.3 Session-aware Chat component: renders the ACTIVE session's
  transcript; composer adapts per type (chat = lean placeholder +
  no ward bar; research = research placeholder + ward context bar +
  budget indicator). Verify: Playwright (chat session vs research
  session render differently; ward bar appears for ward-bound). —
  tests/sessions.spec.ts. The budget indicator ships as the Continue
  bar (+25) near budget; a live budget counter needs a contract
  surface (P2, recorded).

## 4. Review and hardening

- [x] 4.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each group;
  fix P0/P1, P2s fix or record. Verify: ledger zero open P0/P1.
- [x] 4.2 zbot-reviewer + `task verify` green + status WAITING FOR
  HUMAN REVIEW. Verify: reviewer zero open P0/P1; verify exits zero.
