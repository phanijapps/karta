# Findings — add-chat-modes (the session model)

Ledger of review findings and decisions across the change's life.
P0/P1 must be zero-open before archive; P2s are fixed or recorded.

## Phase gates

### Gate 1 — gpt-6.1-terra (groups 1+2: domain/store/contract + routing)

Verdict: boundaries clean (engine isolation, store stdlib-only,
migration guards, routing/ward composition, event emission all
confirmed). 3 P1 + 2 P2 findings — all addressed:

- **G1-1 (P1) Research-run failures returned as success** — FIXED:
  researchpath now reads the root execution's terminal status after
  the drain (`store.ExecutionResult`); `failed` runs propagate a typed
  error in the outcome (rows still ride along); cancelled runs are
  deliberate stops (no error).
- **G1-2 (P1) Research sends bypass serialization** — FIXED:
  per-session busy guard in the chat service (`researchLocked` +
  `ErrSessionBusy`); overlapping sends fail typed; the SSE handler
  probes `SessionBusy` pre-stream → 409 session_busy; pinned by
  TestResearchSessionBusyGuard (-race, count=3).
- **G1-3 (P1) Hand-written session client bindings** — ADDRESSED per
  the house pattern: `shared/api/client.ts` IS the sanctioned typed
  client (every endpoint is a hand-written fetch typed against the
  generated schema — see the module's own header). The substance is
  fixed: session routes are pinned `satisfies keyof paths`, and
  ChatSend/CreateSession request bodies derive from generated
  `operations` types (compile-time drift detection, the MCP section's
  discipline).
- **G1-4 (P2) Send endpoints missing 404** — FIXED: 404 + 409 declared
  on both send operations (SSE busy documented pre-stream), sync gained
  404; codegen rerun, zero drift.
- **G1-5 (P2) Sync treats unknown as empty** — FIXED: `SyncIn`
  resolves the session; unknown ids 404 typed.

### Gate 2 — gpt-6.1-sol (group 3: UI — the session navigator)

Verdict: FSD/runes/no-toggle/navigator semantics/research rendering
confirmed. 2 P1 + 7 P2 findings — all addressed:

- **G2-1 (P1) Remount blanks cached history** — FIXED:
  `loadTranscript` reloads from seq 0 (full replace); regression test
  (leave to Wards, return, history intact).
- **G2-2 (P1) Mid-send switch leaks output / reconciles wrong
  session** — FIXED: `reconcile(sessionId)` takes the captured id;
  volatile reasoning/content and the pending bubble render only while
  `activeId === sendSessionId`; regression test (hanging send, switch,
  no leak, transcript stands).
- **G2-3 (P2) New Research doesn't navigate** — FIXED: the sidebar
  invokes `onselect(activeId)` after creation (closes drawers, shows
  chat, focuses).
- **G2-4 (P2) Late ward-detail overwrite** — FIXED: generation guard
  in the ContextPanel effect.
- **G2-5 (P2) Collapsed New Research unlabeled** — FIXED:
  `aria-label` + title on the button.
- **G2-6 (P2) Request typing** — merged with G1-3.
- **G2-7 (P2) Roster pagination dropped** — FIXED: the store pages
  until not truncated (bounded 5x100).
- **G2-8 (P2) Clear failure shown as success** — FIXED: local clear
  only on success; failure surfaces the error and keeps the
  conversation.
- **G2-9 (P2) Test rigor** — FIXED: cursor-aware per-session
  transcript mocks (404→empty for unknown shells), group-membership +
  aria-current assertions, empty-shell assert on create, layout-tree
  content assert, remount + mid-send regression tests.

## Decisions (recorded during implementation)

- **D1 — Session type immutable, binding mutable.** A session's type
  is set at creation (`EnsureSession(id, type)`; ON CONFLICT ignores
  later type arguments). Ward binding is mutable (`SetSessionWard`),
  driven by ward enters. No mode field exists anywhere.
- **D2 — Store stays stdlib-only.** `internal/store` keeps its
  documented boundary (AGENTS.md: imports only stdlib +
  modernc.org/sqlite): session type/ward are plain strings in rows;
  `domain.SessionType` conversion happens at the edges (chat service,
  handlers).
- **D3 — Research exchanges complete as units.** The engine pipeline
  is not a token stream: `SendStreamIn` routes research sessions to
  the research path before the streaming machinery; the SSE endpoint
  delivers exactly one terminal `chat_exchange` frame (the handler's
  old mode branch is gone — the SERVICE routes).
- **D4 — Quick chat is lean, full stop.** The daemon no longer wires
  the chat ward plane (`chatwards.go` deleted — superseded by the
  session model; the seams `SetExtraTools`/`SetActiveWard` stay,
  pinned by tests). Wards and delegation live in research sessions
  via the engine's own sticky hosts. The lean persona drops custom
  sections (research-protocol composes only via the wrapper's
  research prompt); chitta recall injects as `## Context`.
- **D5 — Unknown sessions fail typed.** `chat.ErrUnknownSession` maps
  to 404 on send (sync + SSE pre-stream) and clear; the default quick
  chat is created on first use.
- **D6 — Engine owns the Active Ward section.** The intent wrapper
  cannot see hosts (it holds the `domain.AgentEngine` interface), so
  the adapter composes `## Active Ward` for hosted ROOT runs
  (`activeWardSection`). This also covers add-ward-hosting's root-run
  half; the child-prompt half remains that change's open task
  (children inherit the host read-only — file tools — which was
  wired here because delegated agents otherwise lost ward file
  access).
- **D7 — Legacy chat-typed engine sessions stay out of the
  navigator.** The migration's DEFAULT 'chat' typed pre-existing
  goal-* rows chat; the sidebar anchors the Chat group on the
  `default` id and skips other chat-typed rows until run-state
  surfacing lands. New engine sessions are research-typed.
- **D8 — Status derivation.** `SessionSummary.status` is derived
  (messages ⇒ completed, else created). Live-run liveness needs
  run-state surfacing (P2-2).

## P2s (recorded, not blocking)

- **P2-1 — Live budget indicator.** The mock's composer shows
  "budget: 14/16"; the Continue bar ships (+25 near budget) but a
  live counter needs a contract surface for remaining budget.
  Follow-up: expose the progress tracker's budget via the session
  events or a session-state endpoint.
- **P2-2 — Live run status.** Same surface would feed a "working…"
  badge for ward bots/roster agents in the context panel.
- **P2-3 — Leave-ward button.** The mock's composer ward strip has a
  Leave button; no endpoint/tool exists (ward_use switches; no
  ward_leave). Recorded for a follow-up; the strip is informational.
- **P2-4 — Research session auto-titling.** Design decision 4 wants
  titles auto-derived from the first exchange's intent; POST
  accepts titles, shells show "Research" until then.

## Final review

(pending — zbot-reviewer + task verify)
