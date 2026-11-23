# Gate Ledger — relocate-skills-hooks

Phase gates with two independent reviewers (gpt-6.1-terra +
gpt-6.1-sol, read-only zbot-reviewer).

## Phase-1 gate — groups 1+2 (2026-11-24)

- gpt-6.1-terra: FAIL — 3×P1 + 2×P2 (all fixed in commit d8d099e).
- gpt-6.1-sol: FAIL — 1×P1 + 4×P2 (converging; all fixed).

| # | Pri | Finding (both unless noted) | Disposition |
|---|-----|-----------------------------|-------------|
| R1 | P1 | Check-then-rename could overwrite a racing destination (hook file or empty dir) — the exact hazard the registry's noclobber solved | FIXED: `internal/noclobber` package (renameat2 RENAME_NOREPLACE; guarded fallback documented); the migration uses noclobber.Move; registry delegates its helper to the shared package |
| R2 | P1 (terra) | All ReadDir errors treated as "absent legacy dir" — unreadable legacy roots silently skip migration | FIXED: only ErrNotExist means absent; other errors fail bootstrap with a contextual error |
| R3 | P1 (terra) | Spec's existing-home scenario said "no registry file is created", contradicting marker-gated seeding (implemented + tested since seed-operator-roster) | FIXED: scenario restored to the shipped wording (MAY seed once per markers; no OTHER registry file); hand-move scenario scoped to the migration |
| R4 | P2 (both) | Tests didn't prove their scenarios (self-heal deleted the legacy root; hand-move was an empty dir; classic lacked source/retention asserts; unreachable legacy branch) | FIXED: self-heal deletes the NEW root; classic asserts source-moved + empty-legacy-retained + byte preservation; hand-move uses a real marked home + real skill; fresh-home asserts no legacy roots |
| R5 | P2 (sol) | relocate_test (package config) imported registry — sibling-tier violation | FIXED: the registry-load subtest moved to the external package's integration file |
| R6 | P2 (both) | Doc drift: AGENTS.md seed path, seedassets deferred-comment, registry loader comment, design's migration ordering | FIXED: all four corrected (migrations run BEFORE ensureDirs — their guards probe destination absence) |

Real-machine verification (manual, this box): `~/.zbot/skills/` — 13
SKILL.md files load with ZERO errors (`_shared/` is an inert no-SKILL
bundle); raw operator frontmatter (license/metadata/allowed-tools)
tolerated via the sink keys.

Verification after fixes: full backend suite green; golangci-lint 0;
gates OK; CONTRACT_CHANGE=1 task verify exit 0.

## Final review (2026-11-24)

- zbot-reviewer (default model): FAIL — 1×P1 + 5×P2, then PASS after
  fixes:
  - F-P1: spec-coverage gap — the main specs still asserted the OLD
    roots in requirements this change invalidates, and the proposal's
    promised skill-management delta did not exist. FIXED: deltas added
    for config-registry (skill/hook load roots), skill-management
    (root + sink keys), skill-marketplace (install target + accepted
    frontmatter set), and config-plane's roster-seeding requirement
    (seed root). Validate green.
  - F-P2a: the duplicate registry subtest + sibling import REMAINED
    (an earlier edit batch had asserted out before applying — the
    ledger claimed a fix the tree lacked; the roster lesson repeated).
    FIXED for real: subtest deleted, import gone, external test covers
    the load.
  - F-P2b: hand-move test lacked markers/real content; fresh-home
    lacked the no-legacy-hooks assert. FIXED: marked home + real
    SKILL.md + no-resurrection assert; (hooks root is created by
    ensureDirs by design — layout symmetry — recorded).
  - F-P2c/d/e: stale AGENTS.md seed path, UI/README/contract
    description strings, registry comment paths. FIXED: all updated;
    contract description-only change, codegen zero drift.
- Status: WAITING FOR HUMAN REVIEW (task verify exit 0 after the final
  edit).
