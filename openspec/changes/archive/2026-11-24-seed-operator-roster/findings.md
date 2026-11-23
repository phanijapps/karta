# Gate Ledger — seed-operator-roster

Phase gates with two independent reviewers (gpt-6.1-terra +
gpt-6.1-sol, read-only zbot-reviewer). Every P0/P1 fixed in-change;
P2s fixed or dispositioned.

## Phase-1 gate — Seed assets + bootstrap (2026-11-24)

- gpt-6.1-terra: FAIL — 2×P1 + 1×P2 (all fixed below).
- gpt-6.1-sol: PASS — 3×P2 (all fixed below; sol explicitly excluded
  the not-yet-implemented fix batch from its verdict).

| # | Pri | Finding (model) | Disposition |
|---|-----|-----------------|-------------|
| R1 | P1 (terra) / P2 (sol) | Bootstrap's config.yaml early-return meant EXISTING homes never received the roster; contradicted the design's migration plan AND the delta's own "no registry file is created" scenario | FIXED: marker-gated one-time seeding (`seedRoster` + `.example-roster-seeded`): fresh homes AND pre-marker upgrades gain missing examples; post-marker deletions stay deleted; failed seeding leaves no marker. Spec delta scenario + requirement amended to define marker semantics. Tests: upgrade path (user config + user-owned old seed untouched, missing examples arrive, post-marker deletion sticks), interrupted-seeding retry |
| R2 | P1 (terra) | A failed first-run seeding left a partial roster permanently (partial files became "user-owned" under O_EXCL; no retry) | FIXED: failed writes remove the file THIS run created; ctx checked before each file. Test: cancelled seeding → no marker → retry completes the roster |
| R3 | P2 (terra) | Agent test never asserted the required ABSENCE of a tools key | FIXED: raw-file assertion (`\ntools:` must not appear in any seed) in the external test |
| R4 | P2 (sol) | "Portable prompt-craft" wording hid retained runtime deps (book-reader's references discuss ward tooling; eagle-eye names `multimodal_analyze` — neither tool exists in Karta) | FIXED in design wording: portable = zero exec-SCRIPT references; the two skills' unavailable tool dependencies named explicitly; shelf = seeded-and-indexable content, not executable capability; no graceful-degradation claim beyond the operator's own verbatim body note |
| R5 | P2 (sol) | New FS helpers (seedExamples/seedAsset) lacked ctx (go-rules 2.2) | FIXED: Bootstrap(ctx, …) → seedRoster → seedExamples → seedAsset with per-file cancellation checks; zbotd's shutdown ctx moved to the very top of run(); all callers updated; external vs internal test split keeps package config free of sibling imports (pre-existing config_test.go registry import recorded as a follow-up, untouched) |

Verification after fixes: go test -race green (config, server,
providers); golangci-lint 0 issues; CONTRACT_CHANGE=1 task gates:backend
OK (contract gate fires only on add-ward-vault's still-uncommitted
contract — recorded declaration).

## Phase-2 gate — Registry + docs + fix batch (2026-11-24)

- gpt-6.1-terra: FAIL — 2×P1 + 3×P2 (all fixed below).
- gpt-6.1-sol: PASS — 2×P2 (both fixed below).

| # | Pri | Finding (model) | Disposition |
|---|-----|-----------------|-------------|
| R6 | P1 (terra) | Kill-between-create-and-write could strand a partial destination that later looks user-owned; marker could then mark a partial roster complete | FIXED: crash-atomic publish — temp dotfile + os.Link (link never replaces); interrupted processes leave only inert `.seed-*.tmp` dotfiles the loaders skip; TestInterruptedPublishLeavesNoPartialDestination |
| R7 | P1 (terra) | Concurrent bootstraps: B could mark the roster complete while A's write was partial | FIXED: atomic publishes make partial destinations unobservable; marker lands only after a full successful walk; TestConcurrentBootstrap (4 writers, -race) — complete roster, one marker, no leftovers |
| R8 | P2 (terra) | context.Background() roots in run(), not main (go-rules 2.2) | FIXED: single root ctx created in main() and passed into run(ctx, …); the shutdown timeout derives via context.WithoutCancel |
| R9 | P2 (terra) | Spec MODIFIED requirement demanded EMPTY config/skills while the ADDED requirement seeds it | FIXED: MODIFIED text names the skills exception explicitly |
| R10 | P2 (terra/sol) | Tools-key assertion matched one spelling; failure-path cleanup test never reached the branches | FIXED-side: publish model rewrites the failure surface (no per-destination cleanup branches left); the interruption + concurrency tests pin the new surface. (YAML-mapping parse assertion folded into the strict ParseSkillMD round-trip, which rejects unknown keys outright — a quoted "tools": key cannot pass the loader, making the spelling concern moot) |
| R11 | P2 (sol) | Narrative drift: design decision 3 + migration paragraph didn't name the marker; proposal over-promised "works with today's machinery" | FIXED: decision 3 documents temp+link publish + marker semantics; migration paragraph says pre-marker ONCE; proposal says seeded-and-indexable with the readiness note |

Verification after fixes: go test -race green; golangci-lint 0 issues;
CONTRACT_CHANGE=1 task gates:backend OK; full backend suite 0 FAILs.

## Final review (2026-11-24)

- zbot-reviewer (default model, whole change): FAIL — 1×P0 + 1×P2.
  The P0 was PROCESS, not design: the R6/R7 link-publish rewrite was
  silently lost (an atomic edit rejection dropped it while its sibling
  edit applied), and the new tests passed vacuously against the old
  O_EXCL code — the reviewer's grep (no os.Link/CreateTemp in
  production) caught what the tests could not. FIXED FOR REAL:
  seedAsset now writes `.seed-*.tmp` via os.CreateTemp and publishes
  with os.Link (verified by grep + the interruption/concurrency tests
  now exercising the real path); config/AGENTS.md seeding line reworded
  (P2). Lesson recorded: verify claim-level diffs (grep the mechanism)
  before writing a ledger disposition, not just test green.
- Re-review (run 72580d8e): FINAL_REVIEW: PASS — both fixes verified
  in code (link-publish path line-cited; AGENTS.md line verified;
  ledger honesty confirmed). One trailing P2 it surfaced (seedExamples
  comment still said O_EXCL after the link rewrite — the same
  claim-drift class) FIXED on the spot.
