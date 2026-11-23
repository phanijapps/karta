# Tasks

## 1. Domain + store + contract

- [ ] 1.1 `domain.SessionType` (`chat` | `research`); sessions store
  gains `type` (rename `mode`) + `ward` columns; migration. Verify:
  store round-trip (create with type, ward binding set/unset, list
  grouped).
- [ ] 1.2 Contract: GET /api/sessions returns sessions with `type` +
  `ward`; POST /api/sessions creates a research session (returns the
  new session); POST /api/sessions/{id}/continue (unchanged).
  Codegen. Verify: zero drift; session list shape tested.

## 2. Session-scoped routing

- [ ] 2.1 Chat service: the send carries `session_id`; the service
  loads the session (type + ward), routes chat sessions lean /
  research sessions through the engine's full pipeline. The ward
  binding pre-loads the ward context (constitution + layout via the
  ward host). Verify: table tests — chat session routes lean,
  research session routes full, ward-bound session has ward context.
- [ ] 2.2 Server: chat-send reads `session_id`; session CRUD
  endpoints (list with type/ward grouping, create research); ward
  binding updates when ward_use/ward_create fires in a session.
  Verify: handler tests (list grouped, create, ward binding update).

## 3. UI — the session navigator

- [ ] 3.1 Sidebar redesigned: session list grouped by type (Chat /
  Ward Bots / Research); active session highlighted; [+ New Research]
  button; ward sessions show ward icon + name; shells show research
  icon. Verify: Playwright (session list renders, groups correct,
  switching works, new research creates a session).
- [ ] 3.2 Context panel (right sidebar): active ward card (canonical
  page excerpt, file list), provisioned skills, agent roster with
  live status. Verify: Playwright (context renders for ward-bound
  session, absent for chat).
- [ ] 3.3 Session-aware Chat component: renders the ACTIVE session's
  transcript; composer adapts per type (chat = lean placeholder +
  no ward bar; research = research placeholder + ward context bar +
  budget indicator). Verify: Playwright (chat session vs research
  session render differently; ward bar appears for ward-bound).

## 4. Review and hardening

- [ ] 4.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each group;
  fix P0/P1, P2s fix or record. Verify: ledger zero open P0/P1.
- [ ] 4.2 zbot-reviewer + `task verify` green + status WAITING FOR
  HUMAN REVIEW. Verify: reviewer zero open P0/P1; verify exits zero.
