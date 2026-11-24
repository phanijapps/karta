# Findings — ward-v2-full-session

## Phase gates

### Gate 1 — gpt-6.1-terra (backend)

Verdict: boundaries clean (engine isolation, lint skip-set scope,
recursive write-once copy, warden membership, root-only MCP mount).
6 P1 + 4 P2 — ALL FIXED:

- **G1-1 (P1) TouchLastUsed precision** — FIXED: surgical
  stampLine (exact top-level key, inline comment preserved,
  prefixed keys untouched, no byte drift elsewhere); absent-vs-error
  read distinction; tests (TestTouchLastUsedSurgical).
- **G1-2 (P1) persona upgrade resurrection** — FIXED:
  upgradePersonaSeeds replaces only ABSENT or byte-identical neutral
  v1 seeds (persona_v1 embedded for comparison); user edits and
  deletions of personalized files never touched.
- **G1-3 (P1) allowlist bypass** — FIXED: the agent tool allowlist
  governs the COMPLETE inventory (ward tools incl. ward_shell, MCP);
  `tools: []` zeroes the whole plane (delegate remains depth-granted,
  matching TestExplicitEmptyAllowlistGrantsNoTools).
- **G1-4 (P1) shell descendants outlive timeout** — FIXED: process
  group (Setpgid) + group SIGKILL in Cancel; TestShellTimeoutKillsGroup
  proves a backgrounded touch cannot land.
- **G1-5 (P1) dead MCP server unrecoverable** — FIXED: Refresh
  restarts dead + changed-spec servers; Tools() reconciles first so
  the next run recovers (spec-key comparison covers config changes —
  also the changed-config P2).
- **G1-6 (P1) ensureWarden hand-written** — FIXED: response typed via
  operations["ensureWarden"] (the house client pattern; route pinned).
- **G1-7..10 (P2)** — FIXED: ward Detail carries the v2 identity;
  MCP name collisions dedupe with a stable fnv suffix (pathological
  double-collisions drop + log); warden 201 atomic via RowsAffected
  (EnsureWardenSession returns created).

### Gate 2 — gpt-6.1-sol (frontend)

Verdict: union/merge/runes/FSD/wiring confirmed. 1 P1 + 2 P2 — ALL
FIXED:

- **G2-1 (P1) ward row could select an ordinary bound session** —
  FIXED: only `ward-<name>` ids are warden rows; other bound sessions
  render after; selectWard never bypasses ensure for non-wardens.
  ROOT CAUSE BONUS: the sidebar's {#each} keyed by ward name DEDUPED
  the two rows — keyed by kind+id now.
- **G2-2 (P2) missing Playwright coverage** — FIXED: the warden test
  (stateful roster: ward row → ensure → active warden; one warden row
  per ward alongside an ordinary bound session).
- **G2-3 (P2) response typing** — FIXED (with G1-6).

## Decisions (recorded during implementation)

- **D1 — Unsandboxed shell, cwd-pinned** (the operator's explicit
  agentzero-parity direction): bash via `bash -lc` with `cmd.Dir` =
  the ward root, env scrubbed to an allowlist + WARD_DIR/WARD_NAME,
  120s default / 300s max timeout, 256KB combined-output cap. No
  container in v1 (recorded trust decision; sandboxing is a follow-up
  if ever wanted).
- **D2 — Non-zero exits are RESULTS, not tool errors** — the model
  reacts to `[exit: ...]`; only refusals (no ward) and setup failures
  error. Timeouts report `[timeout after Ns]` in the output.
- **D3 — The v2 homes are lint-built-ins**: wiki/reports/apps are
  skipped at the lint root for EVERY ward (dot-entries already are),
  so legacy confs stay clean post-migration with zero user-file edits.
  New templates ALSO declare them (optional nodes).
- **D4 — .zbot/config.yaml is user-owned**; the daemon writes only
  `last_used` (TouchLastUsed: comment-preserving single-line replace).
- **D5 — Wardens are research sessions with reserved ids**
  (`ward-<name>`), no new session type; the sidebar merges the ward
  roster with bound sessions — every ward renders exactly one row.
- **D6 — MCP tools mount on ROOT engine runs only** (children inherit
  only via delegation grants); quick chat (the chat service's lean
  plane) never mounts them.
- **D7 — Persona v2 re-seed**: a second write-once marker upgrades
  pre-v2 installs to the operator's SOUL/OS/INSTRUCTIONS set once;
  fresh installs seed directly with both markers; deletions stick.

## Live-verified (the full session)

- **ward_shell**: `echo shell-works && date` → real output, `[54ms in
  stock-analysis]`.
- **Retrieval plane**: `mcp_ddg-search_search` EXECUTED (twice) —
  DuckDuckGo bot-detection blocked the results (provider-side; the
  model retried once and documented it honestly in the wiki entry).
  The plane is proven; brave-search (with key) is the drop-in fix.
- **Wiki discipline**: `wiki/2026-11-23-capability-check.md` — dated,
  structured, honest about the blocked search.
- **Gate bug found live**: ward_shell's Def lacked `Name` → the tool
  was offered UNNAMED (the model: "no tool literally named ward_shell
  but I do have a ward bash executor"). Fixed + pinned by
  TestWardToolSetIncludesShell.

## P2s (recorded, not blocking)

- **P2-1 — DDG bot-detection.** duckduckgo-mcp-server's results are
  blocked (TLS fingerprint). brave-search (API key, configured in the
  old system) drops into the same bridge unchanged.
- **P2-2 — MCP http transports off v1** (stdio only).
- **P2-3 — Per-ward model overrides** (.zbot provider/model) noted in
  design's open questions.
- **P2-4 — Shell sandboxing** (bubblewrap/docker) if the trust posture
  ever changes.
- **P2-5 — ward_entered/ward_created event kinds** remain
  add-ward-hosting's open task.

## Final review

**zbot-reviewer (final gate): 0 P0; 3 P1 + 7 P2 — ALL FIXED on
receipt.**

- **F-1 (P1) warden row lookup keyed by id against a name-keyed map**
  — the real G2-1 root cause surfaced late: byWard was keyed by ward
  NAME but probed by warden ID, so wardens never matched AND ordinary
  bound sessions collided (last-in-roster silently dropped). FIXED:
  byWard maps ward → ALL bound sessions; the warden (id ward-<name>)
  renders first, the ward's other conversations after, deleted-ward
  sessions still render; the Playwright test now pins post-ensure
  rows + reuse-without-re-ensure.
- **F-2 (P1) vacuous group-kill test** — FIXED: the descendant lands
  at ~2s; the test polls to 3s so a regression FAILS.
- **F-3 (P1) warden backend untested** — FIXED:
  TestEnsureWardenSession (atomic created; 8 concurrent callers →
  exactly 1) + TestEnsureWardenHandler (201→200, typed shape, 404).
- **F-4..10 (P2)** — FIXED: persona upgrade semantics aligned with D7
  (marked-home deletions stick; atomic temp+rename replace;
  TestPersonaV2UpgradeMarkedHome); executor coverage (changed-spec
  restart, dead recovery via Tools, collision suffix — which ALSO
  fixed a real ext[:55] panic on short names); spec text corrected
  (root-only MCP, no boot-event claim); shell group-kill split
  unix/other (GOOS=windows builds); TouchLastUsed exact-key append
  gate + no-op cleanup; concurrent-start leak guard; restrictive
  allowlist test + detail assertions.

**task verify: exit 0** (gates + codegen zero drift + lint 0 + full
suites + budget OK + 89 Playwright).

**LIVE full-session smoke (5.2's verify)**: warden run in
stock-analysis — mcp_ddg-search_search executed (DDG bot-blocks
results; plane proven), ward_shell ran `echo shell-works && date`
(54ms, in-ward), wiki/2026-11-23-capability-check.md written with the
honest blocked-search note. Brave-with-key is the drop-in replacement
for DDG's blocking (P2-1).
