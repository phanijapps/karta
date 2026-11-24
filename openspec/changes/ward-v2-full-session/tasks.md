# Tasks

## 1. Ward v2 layout + warden sessions

- [x] 1.1 `internal/wards`: v2 layout (wiki/, reports/, apps/,
  .zbot/config.yaml) in the archetypes + lint nodes (required: false);
  additive migration on boot (dirs created write-once). `.zbot`
  config read (name/description/last_used/agents/skills) + last_used
  touch on entry. Verify: wards tests (template instantiates v2,
  legacy ward migrates clean, config round-trip, last_used updates).
- [x] 1.2 Warden sessions: `EnsureWardenSession(ward)` (id
  `ward-<name>`, research, bound); selection/side effects via the
  existing session surface. Verify: store + handler tests (create
  idempotent, lists under Ward Bots, send routes research).
- [x] 1.3 Persona seeding: soul/os/instructions prompts seeded
  write-once from the operator set; sysprompt slots compose them.
  Verify: config seed tests (3 prompts present, user edits win).

## 2. Execution plane (ward_shell)

- [x] 2.1 `internal/wardtools/shell.go`: the ward_shell tool (LIVE:
  "echo shell-works && date" ran in-ward, 54ms; test suite covers
  cwd/env/caps/timeout/refusal). NOTE the gate bug found live: the
  Def lacked Name → offered with an empty name; fixed + unit-pinned
  (TestWardToolSetIncludesShell).
  (cwd-pinned bash, env allowlist, 120s default/300s max timeout,
  256KB output caps, typed refusal without a ward). Verify: table
  tests (happy run, cwd pinning, timeout, cap truncation, refusal,
  env scrub).
- [x] 2.2 Mount on the hosted plane (root + children); the packet's
  Homes section (reports/wiki/apps discipline). Verify: engine tests
  (tool offered in hosted runs, absent unhosted; children inherit).

## 3. Retrieval plane (MCP execution)

- [x] 3.1 `internal/mcpclient`: stdio execution client (spawn,
  initialize, tools/list, call, restart-on-failure, bounded). Verify:
  client tests over a fake stdio server (list, call, crash-restart,
  timeout).
- [x] 3.2 Engine mounting: ROOT runs gain live MCP tools (LIVE:
  mcp_ddg-search_search executed twice; DDG bot-detection blocks its
  results — provider-side, the tool plane works; Brave-with-key is the
  drop-in fix). Same assert-abort casualty: the mount block initially
  failed to land — now applied + built.
  (mcp_-prefixed); fail-soft when a server is down. Verify: adapter
  tests (tools offered, prefix, absent when unwired); live ddg-search
  smoke.
- [x] 3.3 Codegen/contract: WardSummary + Ward carry
  description/last_used; POST /api/wards/{name}/warden (EnsureWarden,
  200/201 idempotence). Verify: codegen zero drift + handler tests
  (warden route + views).

## 4. UI

- [x] 4.1 Sidebar: the ward's WARDEN session is the ward row (select
  → warden); [+ New Research] unchanged. Verify: Playwright (ward row
  selects the warden session; warden appears once per ward).
- [x] 4.2 Context panel: ward card shows .zbot identity
  (name/description/last_used) when present. Verify: Playwright.

## 5. Review and hardening

- [ ] 5.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol); fix P0/P1, P2s
  fix or record. Verify: ledger zero open P0/P1.
- [ ] 5.2 zbot-reviewer + `task verify` green + live full-session
  smoke (a research run in a ward: searches, runs a script in apps/,
  writes a dated report + wiki entry, persists the answer). Verify:
  reviewer zero open P0/P1; verify exits zero; artifacts on disk.
