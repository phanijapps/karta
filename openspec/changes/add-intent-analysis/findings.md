# Gate Ledger — add-intent-analysis

Phase gates with two independent reviewers (gpt-6.1-terra +
gpt-6.1-sol, read-only zbot-reviewer). Every P0/P1 fixed in-change;
P2s fixed or dispositioned.

## Phase-1 gate — Contract + router + prompts (2026-11-24)

- gpt-6.1-terra: FAIL — 4×P1 + 4×P2 (all fixed or dispositioned).
- gpt-6.1-sol: FAIL — 4×P1 + 1×P2 (converging with terra; all fixed).

| # | Pri | Finding (both unless noted) | Disposition |
|---|-----|-----------------------------|-------------|
| I1 | P1 | Roster-marked existing homes never received the persona seeds → composition would fail on upgrade | FIXED: persona seeds carry their own marker (`.persona-seeded`); roster pair seeds as a unit under its marker; upgrade + lifecycle tests (the fix's first cut had a shared-marker ordering bug — agents' marker write skipped skills — caught by tests before commit) |
| I2 | P1 | Strict decode accepted null/`{}`/partial/trailing JSON as a valid analysis | FIXED: `ParseIntentAnalysis` rejects trailing data and requires primary_intent, complexity, approach, ward action + ward name; malformed responses now take the retry/degrade path instead of influencing execution |
| I3 | P1 | Parseable-but-unusable user templates (missing slots, `{{.Unknown}}`, empty, contract-free prose) were selected and failed the run | FIXED: candidate must parse + carry all four resource slots + render successfully; any failure falls back to the embedded default (five cases tested) |
| I4 | P1 | Blank/whitespace role body produced a heading-only empty prompt | FIXED: blank required role = typed SectionError; blank optional = skip |
| I5 | P2 (terra) | Unreadable optional prompt file indistinguishable from absent | DISPOSITIONED: read/parse failures surface through the registry's existing load-error channel (API/UI observability); composition treats them as absent per the spec's optional-missing scenario — no parallel error path added |
| I6 | P2 (terra) | use_existing ward degradation unreported | FIXED: dropped list gains `ward(use_existing not in index):<name>` |
| I7 | P2 (terra) | Fence extraction could discard a valid response (unrelated fences, same-line fences) | FIXED: brace-matching string-aware scan, fence-agnostic |
| I8 | P2 (sol) | SectionError did not name the file | FIXED: File field (registry path when known, derived `config/prompts/<name>.md` otherwise), asserted in tests |
| I9 | P2 (terra) | Task 1.4 verification gaps (persona lifecycle, seeded composition, ordering, OS portability) | FIXED: TestPersonaSeedUpgradeAndLifecycle, TestSeededPersonaComposes (bootstrap → registry.LoadAll → Compose), explicit-OS + custom-order tests |

Verification after fixes: golangci-lint 0 issues; 19 internal packages
green; task gates:backend OK.

## Phase-2 gate — Resource index (2026-11-24)

- PENDING.
