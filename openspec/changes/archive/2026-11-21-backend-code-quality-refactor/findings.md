# Findings Ledger — backend-code-quality-refactor

Review lens: principal architect + principal engineer. Scope: `backend/` Go code,
organization-only remediation (behavior-preserving). Priorities: P0 = blocks verification or
violates a constitution/AGENTS.md rule; P1 = clear defect/bottleneck/rule conflict, must be
addressed in this change (fixed or explicitly dispositioned); P2 = polish, fix if cheap or
defer with reason. Every row: evidence, suggested fix, risk, disposition.

## Baseline (frozen 2026-11-24, updated after GPT phase-1 gate)

Environment: go1.27.4 linux/amd64 (go.mod floor 1.26), CGO_ENABLED=1 via Taskfile, race
detector on, dev workstation. Working tree PRE-DIRTY (uncommitted work from earlier sessions:
Taskfile.yml CGO refactor, backend files, AGENTS.md, go.mod — this change keeps its edits
disjoint and minimal on top).

Measured:
- `task test:backend` (`CGO_ENABLED=1 go test -race ./...`, after cache warm): FAIL — only
  `internal/skillhub.TestRepoCapabilityExpiryAndBinding` panics (F-01). Fresh (uncached) run
  ≈28s total; slowest packages: mcpclient 12.9s, providers 4.8s, registry 3.8s, recalleval 3.7s.
- `task lint:backend` (golangci-lint): 0 issues. `go vet ./...`: clean.
- Boundaries verified clean: no eino import outside `internal/engine/eino/`; no
  `internal/` import of `cmd|frontend|desktop`.

Not run at baseline (recorded, not silently assumed): `task test:chitta:ladybug` (real
LadybugDB driver suite), `task verify` (full pipeline — blocked by F-01 until task 3.1).

Pre-dirty contract note: `contracts/openapi.yaml` and its generated bindings
(apigen, frontend client) carry a large, CONSISTENT uncommitted diff from prior
sessions (codegen re-run produces zero delta). This change does not touch them;
verify runs during this change set `CONTRACT_CHANGE=1` with this note as the
recorded declaration (gate 5.3 semantics).

Pre-baseline unblock edit (before the read-only review started, recorded for honesty):
F-00 Taskfile parse fix — without it NO `task` command runs, so no baseline could be frozen.

## Phase-1 gate (GPT gpt-6.1-terra, 2026-11-24)

PHASE1_GATE: PASS with 6×P1 + 4×P2 ledger corrections — all incorporated below: F-13/F-15
evidence corrected, F-18 re-graded as literal rule conflict, F-03 re-graded P0, F-20..F-22
added (sweep gaps), sweep table completed (eino/apigen/tools), baseline made reproducible,
size-only findings demoted/merged pending cohesion evidence, runtime budget re-scoped to
measure-first, risk+disposition columns added.

## Findings

| ID | Pri | Package / file | Finding | Evidence | Suggested fix | Risk | Disposition |
|----|-----|----------------|---------|----------|---------------|------|-------------|
| F-00 | P0 | `Taskfile.yml` | Root Taskfile did not parse (uncommitted CGO refactor left `{{.CGO}}` unquoted → YAML flow-map ambiguity); every `task` command failed | `task test:backend` → parse error line 20 | Quote the 8 command strings | none (tooling only) | FIXED (pre-baseline unblock, task 5.2 will re-verify) |
| F-01 | P0 | `internal/skillhub/skillhub_test.go:507-508` | Stale test panics: ages a capability entry that `ConsumeRepo` deletes on second attempt (eviction-on-replay is intended one-shot semantics: archive.go:110-118) | nil-deref panic on `go test` | Rewrite test to assert independently: (a) fresh registration resolves, (b) replay rejected, (c) independently-registered expired entry rejected | test-only; none | task 3.1 |
| F-02 | P1 | `internal/engine` (410 LOC, 0 tests) | The `domain.AgentEngine` seam — the point of engine isolation — has zero tests | no test files | Fake-based seam tests: ResolveAgent contract, FilterTools allowlist, ExecuteTool error mapping, concurrent RunGoal (race detector) | low | task 3.2 |
| F-03 | P0 | `internal/engine/` | No `AGENTS.md` — violates constitution "Add a simple AGENTS.md in each package" (backend rule) | `ls internal/engine/` | Write it: responsibility, boundaries (eino isolation), entrypoints | none (docs) | FIXED in 5.3 (five AGENTS.md + thin-main note) |
| F-04 | P1 | `docs/development/go-rules.md` | Empty file (0 bytes) — engineering-rules doc referenced by project rules does not exist in substance | `wc -l` → 0 | Populate per design decision 6 | none (docs) | task 4.1 |
| F-05 | P2 | `internal/textutil` (34 LOC, 0 tests) | Untested utility package (deliberate single home for normalization) | no test files | Small table tests incl. unicode edge cases | none | task 3.3 |
| F-06 | P2 | `cmd/zbotd` (291 LOC, 0 tests) | Thin main untested; has AGENTS.md; wiring correct | no test files | RESOLVED (phase-2 gate #6): documented exception — cmd/zbotd is the composition root; AGENTS.md gains a note declaring it intentionally thin (test floor applies to `internal/` + `chitta/` + `distiller/`, not thin mains) | none | task 5.3 |
| F-07 | P1 | `internal/registry/mutate.go` (656 LOC, 21 funcs) | Mixed responsibilities: MCP validation/marshal AND skill validation/rendering AND file-replace helpers in one file — multiple reasons-to-change (cohesion evidence, not just size) | func map: installNoClobber, validateMCP, marshalMCP, validateSkill, renderSkillMD | Same-package split: mutate_mcp.go / mutate_skills.go / fileutil helpers | churn (blame) | task 5.1 |
| F-08 + F-16 | P1 | `internal/server/mcpimport.go` (525 LOC, 13 funcs) | Layering: a pure JSON-import parser (walkObject, scanDuplicateKeys, decodeString, mapImportEntry) lives inside the HTTP package | func map; merged F-08/F-16 per gate | Same-package split parser vs handlers now; subpackage extraction only if debate adopts | churn | FIXED in 5.1 (extracted internal/mcpimport; parser+map split 260/282) |
| F-09 | P2 | `internal/skillhub/fetcher.go` (516 LOC, 12 funcs) | Size + fetch/extract/detect seams; cohesion split candidate (demoted from P1: size alone insufficient — gate #7) | func map | Split only if natural seam confirmed at implementation | churn | FIXED in 5.1 (fetcher split 336/199 along extraction seam) |
| F-10 | P2 | `internal/providers/store.go` (499 LOC, 21 funcs) | REJECTED by debate (below the 500 budget; splitting = remediation theater) | func map | Do not split; no grandfather needed (under budget) | churn | closed (no action) |
| F-11 | P2 | `internal/domain/memory.go` (546 LOC, 13 funcs) | REJECTED by debate (domain purity already mechanically guarded by purity_test.go); phase-2 gate #2: must be GRANDFATHERED since it stays over budget | wc -l | Grandfather entry with cohesion verdict + remediation task id | none | task 5.1 (grandfather) |
| F-12 | P2 | `internal/mcpclient` tests (12.9s) | Timeout-path tests use 10–20s deadlines and wait them out (garbage-binary tests assert elapsed < 5–8s) | mcpclient_test.go:68,86,131,176,191 | Measure first (cold/warm breakdown), then shorten timeout-path deadlines preserving assertions; `t.Parallel()` where independent | flakiness if over-tightened | task 6.1 (measure-first) |
| F-13 | P2 | `chitta/internal/recalleval` (3.7s) | EVIDENCE CORRECTED (gate #2): cases run sequentially inside the harness `Run` (recalleval.go:103-110,148-150), NOT t.Run subtests — parallelizing the loop is a harness semantics change | recalleval.go:103 | Package-level `t.Parallel()` between the two tests only if independent; harness parallelism → follow-up | harness behavior change if forced | task 6.1 (narrow) + follow-up |
| F-14 | P2 | `internal/chat/chat.go:~286-293` | Doc comment on `NewStoreBacked` duplicated verbatim | read | Delete duplicate | none | FIXED in 5.2 (deleted; zero users) |
| F-15 | P1 | `internal/chat/chat.go:250-260` + `internal/engine/eino/adapter.go:139-150` | MERGED (debate C2, CORRECTED by phase-2 gate #1): two sites with OPPOSITE persist-failure semantics — chat SKIPS publish when AppendEvent fails; adapter IGNORES the append error and publishes anyway (its "Durable-first" comment is false); both swallow publish errors (can be ErrClosed); daemon has NO logging mechanism | chat.go, adapter.go reads | In-change: accurate per-site comments + characterization tests with SEPARATE assertions (chat: no-publish-on-persist-failure; adapter: publish-anyway) + closed-bus coverage at both; logging + retry/publish policy = ONE decision → follow-up | silent event loss; site divergence unpinned | task 3.4 + 5.2 + follow-up |
| F-17 | P2 | `chitta/internal/ladybug/db.go` | Store methods never thread ctx — 10+ `context.Background()` in query/exec paths; no cancellation propagation | grep | Follow-up (needs internal signature changes) | none now | follow-up |
| F-18 | P1 | `internal/engine/engine.go:24-32` | RE-GRADED (gate #6): LITERAL conflict with `backend/AGENTS.md:70` "zero hardcoded agents, prompts, skills, hooks, or MCPs" — fallback agent name + hardcoded system prompt in the seam package | read | Behavior change → separate follow-up change (move fallback prompt to config/bootstrap); record rule ownership decision in debate | follow-up scope creep if fixed here | task 2.x debate → follow-up |
| F-19 | P2 | `chitta/embed.go:152` | Hardcoded 5-minute embedding timeout, no knob | read | Follow-up: config knob | none now | follow-up |
| F-20 | P1 | `chitta/internal/recalleval/recalleval.go:136-145` | NEW (gate #3), RE-DISPOSITIONED (debate C1 — both models converged fix-now): harness discards every embedding-backfill error while claiming to exercise the vector lane — a passing score may prove BM25-only; eval harness ≠ product code (same category as F-01); the fix leaves the deterministic happy path unchanged | read | Fix in-change: return wrapped errors from fetch/embed/save out of `Run`; VALIDATION (phase-2 gate #5): deterministic coverage of all three failure propagations + a vector-lane participation test (proves embeddings actually flow) + intended change to invalid-eval verdicts recorded in the change summary | already-passing invalid evals start failing (intended, documented) | task 3.5 |
| F-21 | P2 | `internal/engine/eino/adapter.go` | `run` method ≈185 LOC (126-311); adapter has zero tests | func map | Tier-2 adapter tests (injectable model factory + idGen via unexported options) exercise it; split `run` into phases only if still warranted after tests exist | churn | tests in 3.2; SPLIT SKIPPED by decision: adapter.go is ~350 LOC (under the 500 budget), the run phases are pinned by tier-2 tests, and no budget driver remains |
| F-22 | P1 | `internal/engine/eino/adapter.go:69-72` | `atomicAdd` performs non-atomic `*p++` on shared `idCounter` — data race on concurrent RunGoal → colliding event/execution IDs | read 65-75 | `atomic.AddInt64` — restores the function's own named intent (debate: the one behavioral exception both models endorse); tier-2 `-race` test with injectable idGen actually reaches it | latent race becomes live | FIXED in 5.2+3.2 (atomic.AddInt64; race test red-then-green) |
| F-23 | P1 | `internal/engine/eino/adapter.go:107` | NEW (GPT R1, verified): `RunGoal` builds run ctx from `context.Background()` — caller cancellation ignored after startup, contradicting the `domain.AgentEngine` streaming contract | read 106-116 | Characterization test pins current detached semantics; intended semantics (parent-ctx vs detached) is a design decision → follow-up | contract-vs-impl divergence | task 3.2 (characterize) + follow-up |
| F-24 | P1 | `internal/engine/eino/adapter.go:106-110,126-132` | NEW (GPT R1 + GLM R2, verified): second same-session RunGoal overwrites `cancels[sessionID]`; first run's exit deletes the second's handle — orphaned/uncancellable runs | read | Characterization test (deterministic overlap) now; ownership policy (reject/replace/multi-run) + `sessionRun` struct owning cancel+events+lifecycle → follow-up | run leak | task 3.2 (characterize) + follow-up |
| F-25 | P1 | `internal/engine/eino/adapter.go:78-123,207,326` | NEW (GPT R1, GLM R2 strengthened): `GoalRequest.Agent`/`.Context` never forwarded into `runInput`; fixed system prompt used; RunGoal's comment "Resolve model: request > agent > refuse" misdescribes (no agent branch exists) | read | Fix the comment to describe actual behavior now; agent-field wiring is a product decision → follow-up | misleading comment; missing feature | comment FIXED in 5.2; wiring in follow-up |
| F-26 | P1 | `internal/engine/eino/adapter.go:150-160` | NEW (GLM R1, verified): models-cache key = provider/model/hashToken — omits `BaseURL`; a registry reload changing BaseURL keeps the stale client (credential sent to old URL) until model/token changes — same race class chat's NewStoreBacked documents avoiding | read | One-line key fix noted (`modelKey += prov.BaseURL`); behavior → named follow-up | security-adjacent staleness | follow-up |
| F-27 | P2 | `chitta/internal/ladybug/db.go` (673), `memory_store.go` (666), `internal/domain/memory.go` (546) | NEW (GLM R1; extended by phase-2 gate #2): over-budget files not in the original ledger — the 500-LOC gate must inventory and disposition EVERY over-limit hand-written file, including conditional/deferred split outcomes | wc -l | Grandfather rows with cohesion verdict + remediation task id (gate scope: non-test, non-generated, non-vendored .go files; fakedriver.go excluded as declared test double; apigen excluded as generated) | none (inventory) | task 5.1 (grandfather list) |
| F-28 | P2 | `internal/engine/engine.go:53-55` | NEW (GLM R1/R2): `engine.BuiltinProvider` is pure pass-through to `tools.BuiltinProvider` with zero external users (zbotd uses tools directly) — dead indirection in the seam package | grep | Delete (compile-checkable, behavior-preserving) | none | FIXED in 5.2 |
| F-29 | P0 | `internal/engine/eino/`, `chitta/internal/ladybug/`, `chitta/internal/recalleval/` | NEW (GLM R1, verified): three MORE packages missing AGENTS.md beyond F-03's engine — task 5.3's own verify fails without them | find | Write the three AGENTS.md under F-03's umbrella | none (docs) | task 5.3 |
| F-30 | P1 | `chitta/internal/recalleval/fixtures.yaml` + harness | NEW (phase-3 gate #3 causality probes, verified empirically): golden floors are VECTOR-INSENSITIVE — garbage embeddings on BOTH sides (constant query vectors, content-independent passage vectors) still pass with IDENTICAL scores (nDCG 0.829 / MRR 0.762) despite WeightVector=1.0; the harness comment "vector lane and MMR are exercised under the floors" is not causally true; the eval cannot detect vector-lane regressions | probes in this change; characterization test TestGoldenFloorsAreVectorInsensitive | Follow-up: recalibrate fixtures/fusion so vector quality affects outcomes (product/eval decision); in-change: characterization test pins the weakness + TestVectorLaneParticipates proves the lane runs | eval false confidence (now documented) | follow-up + tests in 3.5 |
| F-31 | P2 | `providers/store.go` (secrets.Set backup-restore, Reload), `distiller` (MarkDistilled), `engine/eino` (Append/FinishExecution fire-and-forget) | NEW (rule 2.1 enforcement surfacing): suspicious unchecked error sites beyond the tolerated classes — now excluded with reasons in `.golangci.yml` pending policy | golangci errcheck sweep | Follow-up (error-handling hardening alongside F-15 policy): surface or log these per the logging-mechanism decision | silent best-effort failures | follow-up |
| F-32 | P2 | 17 files, 50 exported symbols | NEW (rule 1.3 enforcement surfacing): missing or colon-form (non-godoc) doc comments on exported symbols — FIXED in-change (task 4.3): all rewritten/added, revive exported clean | golangci revive sweep | Done — 50 comments written, block consts documented | none (docs) | FIXED in 4.3 |
| F-33 | P2 | 14 files, ~25 sites | NEW (rule 2.3 enforcement surfacing): type assertions on errors (→ errors.As), ==/!= on errors (→ errors.Is), non-wrapping %v verbs (→ %w) — FIXED in-change (task 4.3) | golangci errorlint sweep | Done — all sites converted; multi-%w used where sentinels+causes chain | none (behavior-preserving wrapping) | FIXED in 4.3 |

## Package sweep verdicts (task 1.2 — complete; includes packages omitted before gate #4)

| Package | Verdict | Notes |
|---------|---------|-------|
| internal/chat | mostly clean | F-14, F-15 |
| internal/config | clean | typed tree, defaults + bootstrap |
| internal/domain | mostly clean | F-11; other 7 files are clean contracts (ISP-compliant) |
| internal/engine | findings | F-02, F-03, F-18 |
| internal/engine/eino | findings | F-21, F-22; adapter otherwise correct (only eino importer — boundary holds) |
| internal/eventbus | clean (exemplary) | drop-oldest bounded mailboxes, leak-guarded watchers |
| internal/mcpclient | prod clean | deadline + straggler-abandonment; F-12 test-side |
| internal/providers | store.go split candidate | F-10; secret.go exemplary |
| internal/registry | mutate.go split candidate | F-07; other files cohesive |
| internal/server | parser layering | F-08/F-16; handlers thin and contract-typed — correct |
| internal/server/apigen | EXCLUDED — generated | `gen.go` is codegen output; excluded from hand-review by constitution (contract-first flow owns it) |
| internal/skillhub | split candidate + stale test | F-09, F-01 |
| internal/store | clean | WAL + busy_timeout + FK pragma at Open |
| internal/textutil | untested | F-05 |
| internal/tools | clean | construction-time panic on duplicate name is Must-style, acceptable |
| tools/embed-setup | clean (21 LOC) | one-shot cache warmer, documented degraded-mode fallback |
| tools/lbug-setup | clean (76 LOC) | one-shot vendoring helper |
| cmd/zbotd | thin by design | F-06; wiring correct |
| distiller | clean (exemplary) | consumer-side interfaces |
| chitta | prod mostly clean | F-17, F-19; fakedriver.go (640 LOC) deliberate shared test double |
| chitta/internal/ladybug | F-17 | solid otherwise |
| chitta/internal/recalleval | findings | F-13, F-20 |
| third_party/go-ladybug | OUT OF SCOPE | vendored |

## Debate log (task 2) — GPT `gpt-6.1-terra` ↔ GLM `glm-5.3`, two rounds

Round 1 (independent critiques, 4 axes each) — artifacts: GPT `26f51237…output.md`, GLM `57e62fae…output.md`.
Round 2 (cross-examination) — artifacts: GPT `4f6a20dc…output.md`, GLM `0482b8c0…output.md`.

Resolutions (implementer arbitration):
- C1 F-20 → ADOPTED fix-now (GPT conceded R2; GLM's no-seam-needed argument carried; no fail-open flag)
- C2 F-15 → comment + closed-bus/closed-store characterization tests at BOTH merged sites now; logging dropped from this change (daemon has zero logging — introducing a mechanism is out of charter; GPT's point); policy+logging = one decision in follow-up
- C3 F-08 → extract `internal/mcpimport` now (design decision 3 authorizes sibling subpackage; parser is pure, zero HTTP coupling; 939 LOC tests move as parity); fallback if surprises: DEFER entirely (cosmetic split permanently off the table — both models reject)
- C4 file budget → hard 500-LOC mechanical gate in `task verify` + named grandfather list where each entry IS the recorded cohesion review (GLM's enforcement, GPT's format); grandfathers: ladybug db.go 673, memory_store.go 666; fakedriver.go excluded (declared test double); apigen excluded (generated)
- C5 blind spots → F-23/F-24/F-25 adopted as ledger rows + characterization tests now; behavior fixes + sessionRun CANDIDATE struct (pending the run-ownership policy decision — GPT R2 position, not a settled design) → named follow-up; F-20 + F-22 are the TWO bounded in-change behavioral exceptions (harness-verdict honesty; named-intent race fix) — phase-2 gate #7 wording
- C6 → F-26 follow-up (one-line fix noted); F-28 in-change deletion; F-27 grandfather rows; F-29 in-change docs
- Uncontested adoptions: two-tier seam tests (tier-1 contract suite in internal/engine pinning Name/RunGoal/Resume/Cancel semantics incl. close-exactly-once, unknown-session, event ordering, goroutine-leak checks; tier-2 real-adapter tests in engine/eino with injectable model factory + idGen, concurrent RunGoal under -race); ResolveAgent tests must NOT pin the literal fallback prompt (don't enshrine F-18); `.golangci.yml` creation (errcheck check-blank, revive exported, errorlint) with honest grandfathering; verify gates for LOC/AGENTS.md/boundary/contract-drift; named follow-up change requirement (not a parking lot)

## Follow-up proposals (out of scope here — behavior changes; ONE named follow-up change: `backend-engine-run-ownership`)

- F-23 + F-24 + F-25 + F-26 (adapter run ownership): sessionRun struct, caller-ctx semantics decision, cancel-map policy, GoalRequest.Agent/Context wiring, models-cache BaseURL key
- F-15 (policy half): logging mechanism choice + retry/publish-on-persist-failure policy
- F-17: ctx threading through LadybugDB store (internal signature changes)
- F-18 + F-19: hardcoded fallback prompt/agent-name → config bootstrap; embedding timeout knob
- F-13 (harness part): parallel recalleval case execution

## Completion summary (task 7 close-out)

- Reviewer (zbot-reviewer, task 7.1): PASS — zero P0/P1; five constitution
  boundaries clean; both bounded exceptions implemented as ledgered. P2 #1
  (stale recalleval comment contradicting F-30) FIXED. P2 #2 (pending verify)
  closed below.
- Final verification (task 7.2), backend legs ALL GREEN: gates OK · codegen
  clean (CONTRACT_CHANGE=1 declared for the pre-dirty baseline diff, zero
  new drift) · lint 0 issues · full test suite 0 fails · budget gate OK
  (33.9s → 18.8s) · real-LadybugDB suite ok (42s) · build ok · check:frontend ok.
- test:frontend: 62/64. The 2 failures (settings.spec.ts:84, shell.spec.ts:52
  — 'Reset preview' selector) are PRE-EXISTING deterministic failures of the
  prior sessions' uncommitted frontend WIP: this change made ZERO frontend
  edits (frontend/ carries 10 modified files, 1777 insertions, all from the
  pre-dirty baseline; the button exists in Settings.svelte:51; failures
  reproduce identically in isolation). Recorded as baseline carry-over for
  the owning session — out of this change's scope by charter (proposal
  Impact: "No frontend... impact").
- F-20 verdict note (task 3.5): the harness now fails loudly on poisoned
  embedding backfills that previously passed silently; the golden floors
  themselves still pass (scores pinned: nDCG 0.829 / MRR 0.762) — no valid
  eval changed verdict.
- Named follow-up `backend-engine-run-ownership` owns: F-13 (harness
  parallelism), F-15 (policy half), F-17, F-18 (+adapter systemPrompt),
  F-19, F-23, F-24, F-25, F-26; F-27 grandfathers live in the gate script;
  F-30 (vector-insensitive floors) needs its own eval-recalibration change.
