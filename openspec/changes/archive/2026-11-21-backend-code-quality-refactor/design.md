# Design

## Context

The review baseline (verified on this checkout): lint clean, boundaries hold, SQLite opened with
WAL + busy_timeout, `go test ./...` fails in exactly one place — `internal/skillhub` panics in
`TestRepoCapabilityExpiryAndBinding` because the test ages a capability entry that `ConsumeRepo`
now deletes on replay (stale test, intended product behavior). Untested: `internal/engine` (the
`domain.AgentEngine` seam, 410 LOC), `internal/textutil`, `cmd/zbotd`. `internal/engine` has no
`AGENTS.md`. `docs/development/go-rules.md` is an empty file. Hand-written files over ~500 LOC:
`registry/mutate.go` (656), `server/mcpimport.go` (525), `skillhub/fetcher.go` (516),
`providers/store.go` (499), `domain/memory.go` (546). Slowest tests: `mcpclient` 12.9s,
`chitta/internal/recalleval` 3.7s.

## Goals / Non-Goals

**Goals:**
- A prioritized findings ledger (P0/P1/P2) that every later artifact and task traces back to.
- A GPT ↔ GLM adversarial debate that stress-tests the findings and the rules before either is
  applied — debate output is recorded, disagreements are resolved explicitly, not averaged away.
- Minimal-diff, behavior-preserving remediation with exactly TWO bounded exceptions, both
  recorded in the ledger: F-20 (eval-harness verdict honesty — harness-only, happy path
  unchanged) and F-22 (restoring the named atomic intent of `atomicAdd`); `task verify` green
  after each task group.
- A `go-rules.md` that a new contributor can enforce without reading the debate.

**Non-Goals:**
- No API, SSE, on-disk format, or product-behavior changes (see spec: behavior preservation).
- No architecture rewrite, no dependency changes, no test-framework introduction.
- No frontend/desktop work; `svelte-rules.md` stays untouched.
- Genuinely functional product bugs found during review: catalogued in the ledger as follow-up
  proposals, not fixed here.

## Decisions

1. **Ledger-first sequencing.** Produce `findings.md` (P0/P1/P2, each with evidence: file:line,
   why it matters, suggested fix, risk) BEFORE any code edit; tasks 1:1 against ledger items.
   *Why:* a refactor without a frozen findings list drifts into taste-driven churn; the ledger
   also gives the debate a concrete object. *Alternative:* fix-as-you-review — rejected: no
   reviewable diff boundary, no way to prove "only organization changed".

2. **Debate protocol (GPT vs GLM).** Two read-only subagent passes over the same ledger + draft
   rules: one on an `openai` GPT model, one on `zai-coding-cn` GLM (e.g. `glm-5.3`), each told to
   attack the other's positions in a second round; the implementer arbitrates and records each
   resolution (adopted / rejected / deferred, with reason) in `findings.md`. *Why two vendors:*
   same-vendor models share training bias; cross-family disagreement is the signal. *Alternative:*
   single-model review (the existing `zbot-reviewer`) — kept as the final gate, but it cannot
   provide the adversarial second opinion the product brief demands.

3. **Refactors are split-only, along existing seams.** Oversized files are split by moving whole
   functions/types into same-package files (or a sibling subpackage only when a natural boundary
   exists); no signature changes unless unexported. *Why:* same-package file moves are
   diff-minimal and provably behavior-preserving; subpackage extraction changes import graphs and
   risks cycles. *Alternative:* full package re-layout — rejected as non-behavioral churn.

4. **Engine-seam tests pin the contract, not the adapter.** Tests for `internal/engine` exercise
   the `domain.AgentEngine` seam through fakes (concurrent Run, stream events, error propagation)
   and must not import `github.com/cloudwego/eino`. *Why:* the seam is what future engine swaps
   rely on; adapter internals are eino's problem. The eino adapter's own wiring stays thin.

5. **The stale skillhub test is fixed to assert current intent.** `ConsumeRepo` deletes on replay
   by design (one-shot capability + eviction); the test re-registers a preview before aging it.
   The test gains a comment citing the intended semantics. *Why:* the product behavior matches the
   documented one-shot contract; the test predates eviction-on-replay.

6. **Rules doc taxonomy.** `go-rules.md` gets five sections (naming & size, errors & context,
   concurrency, testing, review gates); each rule = one sentence + rationale + enforcement
   mechanism. Rules must be checkable by lint, test, `task verify`, or reviewer checklist —
   aspirations are banned.

7. **Slow-test budget, measure-first.** Profile cold and warm `task test:backend` runs
   separately (compile/link vs execution vs machine variance) BEFORE changing anything;
   then tighten `mcpclient` timeout-path deadlines and add `t.Parallel()` where isolation
   allows, preserving every probe assertion. The budget recorded in `go-rules.md` carries a
   machine qualifier. `recalleval` harness-internal parallelism is OUT (harness semantics
   change — F-13); only package-level test parallelism is in scope.

## Risks / Trade-offs

- [Refactor introduces a subtle behavior change] → Each ledger item lands as its own commit;
  `task verify` + full test suite run between items; zero unexported-signature changes unless a
  test pins the new shape.
- [Debate becomes an echo chamber / stalls the change] → Two rounds max, then implementer
  arbitrates; unresolved items are recorded as deferred with reason, never silently dropped.
- [Splitting files churns git blame] → Prefer new files named after their content
  (`mutate_skills.go`), keep original files for their remaining core; `git log --follow` survives
  same-package moves.
- [Rules doc ossifies into dead documentation] → Every rule names its enforcement mechanism;
  unenforceable rules are rejected in debate; doc reviewed in the same change that alters any
  encoded rule (spec scenario).

## Migration Plan

Not applicable — no deployable state changes. Rollback = git revert of the change's commits;
nothing in `~/.zbot/` or the contract is touched.

## Open Questions

- Exact GPT model id for the debate round — RESOLVED at apply time by the operator:
  `openai/gpt-6.1-terra` for every phase-review gate and the GPT side of the debate.
- Whether `cmd/zbotd` (291 LOC thin main) warrants a test or an AGENTS.md note declaring it
  intentionally thin — debate item, does not change task structure.
