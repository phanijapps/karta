# Tasks

## 1. Baseline and findings ledger (read-only)

- [x] 1.1 Freeze the baseline: run `task test:backend`, `task lint:backend`, and `git status` in
  `backend/`; record the known `skillhub` failure and current suite wall-clock per package in
  `openspec/changes/backend-code-quality-refactor/findings.md`. Verify: findings.md exists with
  the recorded baseline numbers.
- [x] 1.2 Review every `backend/` package with a principal-architect/principal-engineer lens
  (bottlenecks, clean-code violations, defects; include concurrency hot spots, error/context
  discipline, lock scopes, oversized files, test gaps, doc gaps) and write each finding to
  findings.md as P0/P1/P2 with `file:line` evidence, why it matters, suggested fix, and risk.
  Verify: every package under `backend/` has at least one finding or an explicit "clean" verdict.

## 2. GPT ↔ GLM adversarial debate

- [x] 2.1 Round 1 — independent critiques: launch two read-only subagents over findings.md and
  the draft rules outline, one on an `openai` GPT model, one on `zai-coding-cn` GLM, each told to
  challenge severity, proposed fixes, and rule enforceability. Verify: both critiques are appended
  to findings.md with model attribution.
- [x] 2.2 Round 2 — cross-examination + arbitration: feed each model the other's critique, collect
  rebuttals, then the implementer records a resolution per contested finding (adopted / rejected /
  deferred, with reason). Verify: every contested finding has a recorded resolution; no silent
  averaging.
- [x] 2.3 Promote the resolved ledger: re-prioritize findings.md so tasks 3–6 map 1:1 to adopted
  items; deferred items move to a "Follow-up proposals" section. Verify: each adopted item names
  the task that resolves it.

## 3. Test suite green and test gaps closed

- [x] 3.1 Rewrite the stale `internal/skillhub` test `TestRepoCapabilityExpiryAndBinding` to
  assert independently (gate #8): (a) fresh registration resolves, (b) replay rejected,
  (c) an independently-registered expired entry rejected — with a comment stating the
  one-shot semantics. Verify: `go test ./internal/skillhub/` passes.
- [x] 3.2 Two-tier engine tests (phase-2 gate #3 corrections): tier 1 — a REUSABLE contract
  suite exported from `internal/engine` (e.g. `engine.ContractSuite(t, makeEngine)`) pinning the
  `domain.AgentEngine` surface (Name/RunGoal/Resume/Cancel): close-exactly-once on done/error/cancel,
  unknown-session Cancel, event ordering/identity, goroutine-leak check; run it against a test fake
  AND, from `internal/engine/eino`'s test, against the REAL adapter; unit tests for `ResolveAgent`
  (assert error MESSAGE content — it is fmt.Errorf, not a typed error; assert non-empty
  SystemPrompt, NEVER the literal F-18 prompt text), `FilterTools` allowlist, `ExecuteTool` error
  mapping; tier 2 — adapter tests with injectable model factory via unexported options, running
  concurrent RunGoal under `-race` exercising the DEFAULT id generator (F-22 path not replaced),
  plus explicit characterization cases: F-23 (caller ctx detached after RunGoal returns) and
  F-24 (same-session second RunGoal overlap). Verify: both packages pass `go test -race`; tier-1
  suite imports no eino packages; suite green against fake AND real adapter.
- [x] 3.3 Add unit tests for `internal/textutil` covering its public functions. Verify:
  `go test ./internal/textutil/` passes and the package no longer reports `[no test files]`.
- [x] 3.4 F-15 characterization tests with SEPARATE per-site assertions (phase-2 gate #1): chat
  site — no publish after AppendEvent failure; adapter site — publish attempted despite append
  failure; both sites — closed-bus (→ `ErrClosed`) coverage. Verify: tests pass and fail if either
  site's persist-failure behavior silently changes.
- [x] 3.5 F-20 fix: recalleval backfill errors (fetch/embed/save) wrap and return out of `Run`
  (fail-closed; happy path unchanged). VALIDATION (phase-2 gate #5, not conditional): deterministic
  coverage of all three failure propagations; a vector-lane participation test proving embeddings
  actually flow on the happy path; the intended change to invalid-eval verdicts recorded in the
  change summary. Verify: `go test ./chitta/internal/recalleval/` green; poisoned embedding path
  fails the run; happy-path score unchanged.
- [x] 3.6 Regression tests land with each defect fixed in groups 5–6, in the same commit. Verify:
  each fixed ledger item references its regression test in findings.md.

## 4. Engineering rules document

- [x] 4.1 Write `docs/development/go-rules.md` from the debate-resolved outline: naming & file-size
  budgets, error & context discipline, concurrency & locking rules, test floors & runtime budgets,
  review gates — each rule one sentence + rationale + enforcement mechanism (lint / test /
  `task verify` / reviewer checklist), incorporating the debate's rule proposals (errcheck
  blank-discard, ctx-first-param allowlist, %w+package-prefix, race always on, named-mutex owner,
  channel-close ownership, ≥1 test file per package, no live network, measured budgets,
  automated boundary/AGENTS.md/contract-drift gates). Verify: document has no empty sections and
  every rule names its enforcement.
- [x] 4.2 Cross-check the rules against the current codebase: for each rule, confirm it is either
  currently satisfied, mechanically grandfathered (4.3), or tracked by an adopted ledger item
  (groups 3, 5). Verify: findings.md lists the rule→task mapping with no orphan rules.
- [x] 4.3 Create the enforcement the rules require (currently missing): `.golangci.yml`
  (errcheck check-blank with a small justified exclusion list, revive exported, errorlint) with
  honest grandfathering of existing hits; and verify-gate scripts wired into `task verify`:
  500-LOC hand-written-file gate (scope: non-test, non-generated, non-vendored, non-test-double —
  declared test doubles excluded BY NAME in the gate config, starting with `chitta/fakedriver.go`;
  grandfather list per F-27/5.1 — complete inventory, no orphans), every-package-has-AGENTS.md gate, and automated
  boundary checks (no eino outside engine/eino, no internal→cmd/frontend/desktop). Verify: gates
  run red/green correctly on seeded violations; `task verify` green on the tree.

## 5. Behavior-preserving refactors and package docs (per adopted ledger)

- [x] 5.1 Refactors per the adopted ledger, one commit each with `task test:backend` green
  between: (a) extract `internal/mcpimport` from `server/mcpimport.go` — define the exported parser
  boundary (entry types + document error), move/adapt ONLY parser-level tests; handler/guard/
  marketplace tests STAY in `server` (the 939-LOC test file mixes them — phase-2 gate #4); if the
  boundary proves too invasive, DEFER entirely and grandfather the file; (b) split `registry/mutate.go`
  along its MCP/skill/file-helper seams (same-package files); (c) split `skillhub/fetcher.go` only if
  the seam confirms natural — if declined, grandfather it in the same commit; (d) split `engine/eino`
  adapter `run` into phases only if still warranted after tier-2 tests. REJECTED by debate: `providers/store.go`
  and `domain/memory.go` splits. Gate inventory rule (phase-2 gate #2): at change end, EVERY
  over-limit hand-written file is split or on the grandfather list with a cohesion verdict — scope
  excludes test doubles named in the gate config (`chitta/fakedriver.go`) alongside test/generated/
  vendored files; starting grandfather list: ladybug db.go 673, memory_store.go 666,
  domain/memory.go 546. Verify: 4.3 gate green; suite green after every commit.
- [x] 5.2 Adopted clean-code fixes, enumerated EXACTLY (phase-2 gate #6 — no open-ended clause):
  F-22 `atomic.AddInt64` (bounded behavioral exception: restores named intent); F-28 delete dead
  `engine.BuiltinProvider`; F-14 delete duplicated comment; F-25 fix the lying RunGoal comment;
  F-15 accurate per-site comments (chat vs adapter semantics). Each with its regression test in
  the same commit (3.6). Any further item must be added to the ledger first. Verify: `task
  test:backend` and `task lint:backend` green after each item.
- [x] 5.3 Write `AGENTS.md` for every package missing one: `internal/engine` (F-03), plus
  `internal/engine/eino`, `chitta/internal/ladybug`, `chitta/internal/recalleval` (F-29); add the
  F-06 note to `cmd/zbotd/AGENTS.md` declaring it an intentionally-thin composition root exempt
  from the test floor; review the `AGENTS.md` of every package touched by this change. Verify:
  the 4.3 AGENTS.md gate passes and the thin-main note exists.
- [x] 5.4 Confirm zero contract drift and unchanged behavior: `git diff --stat contracts/` is empty
  and `task verify` is green. Verify: codegen reports no uncommitted drift.

## 6. Test-suite runtime budget

- [x] 6.1 Measure first (gate #9): profile cold and warm `task test:backend` (compile vs
  execution breakdown). Then tighten `internal/mcpclient` timeout-path deadlines preserving
  every assertion, and add `t.Parallel()` only where independent (mcpclient tests;
  `recalleval` package-level only — harness loop is out of scope per F-13). Verify: identical
  assertion coverage; budget recorded in go-rules.md with machine qualifier; suite measurably
  faster than the ~28s fresh baseline.

## 7. Review and hardening

- [x] 7.1 Run the read-only `zbot-reviewer` subagent over the change scope with the change
  artifacts and the five constitution boundaries; fix every P0/P1 finding, fix or record P2s in
  the change summary. Verify: reviewer report shows zero open P0/P1.
- [x] 7.2 Re-run `task verify` after all fixes; mark complete only when green. Verify: `task
  verify` exits zero with no skipped tests.
