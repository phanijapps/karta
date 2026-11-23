# Gate Ledger — add-ward-vault

Phase gates run with two independent reviewers per task group
(gpt-6.1-terra + gpt-6.1-sol, read-only zbot-reviewer). Every P0/P1 is
fixed in-change; P2s are fixed or dispositioned here. Task 5.1's
zbot-reviewer pass covers the whole change at the end.

## Phase-1 gate — Vault + schema (2026-11-24)

- gpt-6.1-terra: FAIL — 5×P1 + 3×P2.
- gpt-6.1-sol: FAIL — 3×P1 + 6×P2 (five findings converged with terra).

| # | Pri | Finding (both models unless noted) | Disposition |
|---|-----|------------------------------------|-------------|
| G1 | P1 | Constitution lacks the vault exception (terra; sol agrees planned-for-4.1 is not a phase-1 blocker) | Planned: task 4.1 lands the scoped exception in the same change |
| G2 | P1 | seedFile Stat→WriteFile race can truncate a user file; dangling symlink followed (terra+sol F2) | FIXED: O_CREATE\|O_EXCL seeding; ErrExist = user-owned; dangling-symlink test added |
| G3 | P1 | Customized deprecated wards.root returned untyped error (terra+sol F1) | FIXED: typed DeprecatedWardsRootError; test asserts errors.As |
| G4 | P1/P2 | Node ids made mandatory though design says `id?` (terra P1, sol F2 P2) | FIXED: ids optional; findings identify id-less nodes by match pattern (contract documents this) |
| G5 | P1/P2 | Detail follows symlinks / accepts files — membership ≠ Index (terra P1, sol F8) | FIXED: Lstat + IsDir membership; symlink + plain-file tests |
| G6 | P2 | Malformed glob breaks the whole Index (terra; sol F6) | FIXED: glob validated in ParseConf → ConfParseError naming the match field |
| G7 | P2 | Nested canonical node resolved at wrong directory (terra; sol F5) | FIXED: canonical convention constrained to root-level conf children (documented in wards.go + AGENTS.md); nested-canonical test pins no-title behavior |
| G8 | P1/P2 | Resolver tests Unix-only (HOME, /tmp paths) — could touch real Documents on Windows (terra P2, sol F3 P1) | FIXED: HOME+USERPROFILE set, filepath-derived absolute fixtures, resolved-root asserted before Bootstrap |
| G9 | P2 (sol F7) | titleOf slurped whole file then truncated | FIXED: io.LimitReader(64 KiB) bounded read |
| G10 | P2 (sol F9) | New FS APIs without ctx contradict go-rules 2.2 signature shape | FIXED: ctx threaded through Open/Bootstrap/Index/Detail/Lint with cancellation checks between entries; daemon's shutdown ctx created before wiring |

Verification after fixes: `go test -race` green (wards, config, server),
golangci-lint 0 issues, `CONTRACT_CHANGE=1 task gates:backend` OK.

## Phase-2 gate — Contract + handlers (2026-11-24)

- gpt-6.1-terra: FAIL — 2×P1 + 2×P2 (all fixed below).
- gpt-6.1-sol: FAIL — 1×P1 + 1×P2, after a frozen re-review of the
  post-terra-fix tree (both fixed below).

| # | Pri | Finding (model) | Disposition |
|---|-----|-----------------|-------------|
| G11 | P1 (terra) | User data can exceed contract caps (canonical_title 256, archetype 64, node id 128, match 256) | FIXED: conf-field length limits enforced in ParseConf; canonical title capped at 256 runes (rune-aware, multi-byte tested) |
| G12 | P1 (terra) | Root node accepted `format` → invalid enum emitted | FIXED: root format must be empty (validateNode); tested |
| G13 | P2 (terra) | 500/503 responses undeclared in contract | FIXED: both ops declare Error responses; codegen re-run; detail-503 tested |
| G14 | P2 (terra) | provStore.Close deferred after vault bootstrap early-returns | FIXED: defer moved to immediately after store creation |
| G15 | P1 (sol) | Residual bound mismatches: 129-char ward names, oversized diagnostics, vault_root/path bounds | FIXED: contract name 128→255 (round-trips what filesystems produce), vault_root→2048, finding path→1024; diagnostics + finding text clamped at emit (1024 runes) with regressions for long names and huge unknown-field diagnostics |
| G16 | P2 (sol) | wards/AGENTS.md still said "scans take no ctx" after G10 | FIXED: documents ctx-first I/O with between-entries cancellation |

Verification after fixes: codegen regenerated (zero drift),
wards/server/config tests green, golangci-lint 0 issues, build OK.
Both gates' blockers resolved; phase-5's full reviewer pass re-checks
the whole change.

## Phase-3 gate — Frontend (2026-11-24)

- gpt-6.1-terra: FAIL — 3×P1 + 3×P2 (all fixed).
- gpt-6.1-sol: FAIL — 1×P1 (same as terra's detail race) + 5×P2
  (fixed or dispositioned).

| # | Pri | Finding (model) | Disposition |
|---|-----|-----------------|-------------|
| G17 | P1 (both) | Detail responses could overwrite the selected ward's findings (no request fencing) | FIXED: request-generation token + detail cleared on selection; out-of-order Playwright regression (slow first ward never lands) |
| G18 | P1 (terra) | Configuration nav dead after visiting Wards (showPlaceholder didn't reset view) | FIXED: showPlaceholder resets view='chat'; nav-exclusivity test |
| G19 | P1/P2 (terra/sol) | Failures masqueraded as empty vault / endless loading | FIXED: listWards returns null on failure; explicit list-error and detail-error states with retry; both mocked |
| G20 | P2 (both) | aria-label hid roster metadata from the accessible name | FIXED: content-derived name (badges included) + sr-only "Open ward X"; specs match by regex |
| G21 | P2 (terra) | Mobile nav didn't move focus to the new page | FIXED: showWards/showSchedules/showPlaceholder await tick() then focus main (showChat pattern) |
| G22 | P2 (both) | Unparseable conf described as "freeform" | FIXED: invalid_conf findings distinguish "failed to parse" from conf-less freeform; spec added |
| G23 | P2 (sol) | Undefined --bg-inset/--tx-* tokens with dark literals — unreadable in light mode | FIXED in Wards.svelte: theme-aware --ds-color-* tokens. Sibling features (schedules, chat) carry the same PRE-EXISTING pattern — out of this change's scope; follow-up recorded |
| G24 | P2 (sol) | Ward tests could leak /api/models traffic to a real daemon | FIXED: mockChat mocks /api/models + registers a catch-all /api abort guard FIRST (Playwright consults latest-registered routes first) |
| G25 | P2 (sol) | Roster test didn't assert the clean badge | FIXED: per-card assertions for clean badge and finding count |

Verification after fixes: svelte-check 0 errors; playwright 82 passed
(9 ward specs incl. race, failure, invalid-conf, nav-exclusivity).

## Phase-4 gate — Constitution + docs (2026-11-24)

- gpt-6.1-terra: FAIL — 2×P1 + 1×P2 (all fixed below).
- gpt-6.1-sol: FAIL — 1×P1 (same as terra's first) + 1×P2 (links; same
  as terra's second, graded P1 there).

| # | Pri | Finding (model) | Disposition |
|---|-----|-----------------|-------------|
| G26 | P1 (both) | Constitution asserted "vault lives OUTSIDE ~/.zbot" but ResolveVaultRoot accepted roots inside the daemon home | FIXED: ResolveVaultRoot(configured, daemonHome) enforces it — typed rejection of the home itself, descendants, tilde spellings, AND symlink aliases (symlinks resolved through the deepest existing ancestor); unrelated `.zbot` dirs and `~/.zbot-wards` siblings stay allowed; zbotd passes the resolved home; table + alias tests |
| G27 | P1/P2 (terra/sol) | Both new exploration-doc links were broken (`../changes/…` instead of `../../openspec/changes/…`) | FIXED: links corrected |
| G28 | P2 (terra) | Constitution's bootstrap exception omitted the .gitignore starter write | FIXED: wording now names templates + .gitignore starter, write-once |

Verification after fixes: wards/cmd/server tests green, golangci-lint 0
issues; grep — zero stale Code Wards placeholder references (remaining
mentions are historical/explanatory).

## Phase-5 — final review + verify (2026-11-24)

- zbot-reviewer (default model, whole change, read-only): first pass
  FAIL — 1×P1 + 4×P2, all fixed:

| # | Pri | Finding | Disposition |
|---|-----|---------|-------------|
| G29 | P1 | Landed config-plane spec still REQUIRED ~/.zbot/wards in three SHALL clauses (relocation, bootstrap, self-heal) — contradicting the implementation and the new exception | FIXED: all three clauses drop wards/; spec notes the vault resolves outside the home via wards.vault_root |
| G30 | P2 | architecture-brief home tree still showed wards/ + described internal/wards as Code Ward sandboxing | FIXED — with a wrinkle: the first edit was a silent no-op (whitespace-mismatched replace, unasserted) and the re-check pass caught it; applied for real (row removed, map line rewritten, grep-verified) |
| G31 | P2 | settings-shell spec + Settings fixture said "wards root" (deprecated key) | FIXED: "wards vault root" in both |
| G32 | P2 | api-contract additive-evolution rule requires a minor version bump for new endpoints — version stuck at 0.1.0 | FIXED: bumped to 0.2.0, codegen re-run (noting earlier endpoint additions had also skipped bumps — systemic drift now corrected at this change) |
| G33 | P2 | wards/AGENTS.md documented the pre-G26 one-arg ResolveVaultRoot | FIXED: two-arg signature + the enforced home-containment rule documented |

Final `task verify` re-run after fixes; the reviewer's zero-open-P0/P1
condition was confirmed by a scoped re-check pass: G29/G31/G32/G33
verified fixed, G30's silent no-op caught and truly applied. The change
closes with zero open P0/P1.

