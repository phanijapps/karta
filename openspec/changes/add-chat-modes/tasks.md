# Tasks

## 1. Domain + contract

- [ ] 1.1 `domain.ChatMode` (`chat` | `research` with alias
  normalization `fast`→chat, `deep`→research); the sessions store
  gains a `mode` column (default `chat`); the session read API
  exposes it. Verify: store round-trip + alias table tests.
- [ ] 1.2 Contract: chat-send gains `mode` (enum chat|research,
  default = session's last); `POST /api/sessions/{id}/continue` with
  `additional_iterations` (default 25, 409 when the run is not
  active); session read exposes `mode`. Codegen. Verify: zero drift;
  alias normalization tested.

## 2. Chat service: the mode branch + the research path

- [ ] 2.1 The mode branch: chat mode composes Role+Soul+OS+Tools+
  Context (skips mode-gated custom sections incl. research-protocol;
  no ward tools; thinking off; temperature 1.0). Research mode
  ROUTES through the wrapped engine (the /api/goals path): the
  message becomes the goal; the wrapper handles analysis, skills
  provision (## Skills), ward tools, delegation (delegate_agent),
  and progress budgets; the result lands in the chat session's
  timeline. Verify: table tests — lean shape (no research sections,
  no ward tools, thinking off), research shape (Task Analysis,
  Skills, ward tools, delegate_agent offered), mode persistence.
- [ ] 2.2 The chat-send handler reads the mode (request > session
  default > `chat`), persists the new mode. The handler maps the
  mode to the correct execution path. Verify: handler tests
  (default, explicit switch, persistence, research routes through
  the engine).

## 3. Continue protocol + engine wiring

- [ ] 3.1 The engine adapter exposes a per-session extend closure;
  the continue endpoint grants iterations through it into the
  progress tracker. Verify: adapter tests (extend mid-run, 409 when
  finished).
- [ ] 3.2 Research-mode chat exchanges consume the Continue protocol:
  a near-budget exchange surfaces the Continue affordance. Verify:
  integration test (research exchange → budget approach → continue
  extends → run completes).

## 4. UI

- [ ] 4.1 Mode toggle in the composer (Chat | Research segmented
  control; state per session; visually distinct states). Verify:
  Playwright (toggle, persistence, visual state).
- [ ] 4.2 Task Analysis card on research exchanges (approach badge,
  complexity, ward recommendation, provisioned skills chips, dropped
  recommendations) — the Timeline already streams the event; the
  card renders from it. Verify: Playwright (card appears on
  research exchange with the analysis; absent on chat).
- [ ] 4.3 Ward context footer + Continue button: when a ward is
  active, the composer shows the ward name; when a research run
  approaches its budget, a Continue button offers +25. Verify:
  Playwright (footer with ward, Continue button on near-budget,
  absent on chat).

## 5. Review and hardening

- [ ] 5.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each group;
  fix P0/P1, P2s fix or record. Verify: ledger zero open P0/P1.
- [ ] 5.2 zbot-reviewer + `task verify` green + status WAITING FOR
  HUMAN REVIEW. Verify: reviewer zero open P0/P1; verify exits zero.
