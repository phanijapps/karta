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

## 2. Chat service mode branch

- [ ] 2.1 The mode branch: chat mode composes Role+Soul+OS+Tools+
  Context (skips mode-gated custom sections incl. research-protocol;
  no ward tools; thinking off; temperature 1.0). Research mode on the
  chat surface: full composed stack + ward tools + delegation-ready.
  Verify: table tests (lean shape, full shape, mode persistence,
  memory in both modes).
- [ ] 2.2 The chat-send handler reads the mode (request > session
  default > `chat`), persists the new mode on the session. Verify:
  handler tests (default, explicit switch, persistence).

## 3. Continue protocol

- [ ] 3.1 The engine adapter exposes a per-session extend closure;
  the continue endpoint grants iterations through it into the
  progress tracker. Verify: adapter tests (extend mid-run, 409 when
  finished).

## 4. Review and hardening

- [ ] 4.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each group;
  fix P0/P1, P2s fix or record. Verify: ledger zero open P0/P1.
- [ ] 4.2 zbot-reviewer + `task verify` green + status WAITING FOR
  HUMAN REVIEW. Verify: reviewer zero open P0/P1; verify exits zero.
