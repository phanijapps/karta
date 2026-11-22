# Go Engineering Rules — backend/

These rules govern `backend/` Go code. Each rule is one sentence, carries its
rationale, and names its enforcement — a rule without enforcement is dead paper
and does not belong here. The rules codify the review that produced change
`openspec/changes/backend-code-quality-refactor/` (findings ledger + GPT↔GLM
debate); the debate log there is the rationale archive.

Enforcement keys: **[gate]** = script wired into `task verify`; **[lint]** =
golangci rule in `backend/.golangci.yml`; **[test]** = pinned by a test;
**[reviewer]** = zbot-reviewer checklist item (max one per section — more
reviewer-only rules than that is aspiration).

## 1. Naming and file-size budgets

1.1 No hand-written non-test `.go` file exceeds **500 LOC**. **[gate]**
   *Why:* past ~500 LOC a file starts owning several reasons-to-change (see
   `registry/mutate.go` pre-split). Why not smaller: the budget exists to force
   a conversation, not automatic splits — a file over budget is either split
   along a natural seam or added to the grandfather list with a recorded
   cohesion verdict and a remediation task.
   *Scope:* excludes generated code (`internal/server/apigen/`), vendored code
   (`third_party/`), test files, declared test doubles (currently
   `chitta/fakedriver.go`), and the `tools/` one-shot mains.
   *Grandfather list (live in `scripts/backend-gates.sh`):*
   `chitta/internal/ladybug/db.go` (673 — F-17 ctx-threading will reshape it),
   `chitta/internal/ladybug/memory_store.go` (666 — same),
   `internal/domain/memory.go` (546 — purity-guarded contracts+helpers).

1.2 No `util`/`common`/`helpers`/`misc` packages or files; a file's name
   names its content (`mutate_skills.go`, not `mutate_misc.go`).
   **[gate]** (banned generic path segments) **+[reviewer]** (name QUALITY
   beyond the banned list is judgment, not grep)

1.3 Every exported symbol has a doc comment starting with the symbol name.
   **[lint]** (`revive:exported`)

## 2. Error and context discipline

2.1 Never blank-discard an error (`_ = f()`) outside test doubles — if a
   failure is deliberately tolerated, the discard matches a documented
   exclusion class in `backend/.golangci.yml`. **[lint]}
   (`errcheck` with `check-blank: true` + exclusion classes)
   *Why:* three live defects (F-15 chat, F-15 adapter, F-20 eval) were exactly
   this pattern. The exclusion CLASSES (each with its reason in the config):
   fire-and-forget durable-first emits (`bus.Publish`, `st.Append`,
   `st.AppendEvent`, `st.FinishExecution` — F-15 follow-up owns policy);
   best-effort cleanup (`.Close(`, `os.Remove`, zip/`Add`, glob/walk);
   JSON of internally-built shapes + SSE `Fprintf` to dying connections;
   `ROLLBACK` on failing tx paths; best-effort metadata (`secrets.Set`
   backup-restore, `Reload`, `MarkDistilled`, `time.Parse`,
   `storeSessionList`, `SetDefaultModel`, `UserHomeDir` — F-31 tracks
   hardening). Exclusions are class-wide: a NEW discard that merely matches
   a class is not automatically acceptable — the reviewer checks the class
   still fits (see 5.4).

2.2 Any function doing I/O takes `ctx context.Context` as its first parameter;
   `context.Background()`/`context.TODO()` are allowed only in `main`, tests,
   and goroutine-worker tops. **[gate]** (literal Background/TODO grep;
   every surviving site carries an inline `// ctx-ok: <ledger-id>` marker
   naming its owning follow-up — currently F-15 chat+adapter emits, F-19
   embedding worker, F-23 detach, plus the eval-harness root; the
   `chitta/internal/ladybug/` directory is exempt wholesale pending F-17)
   **+[reviewer]** (signature shape — "new I/O function takes ctx first" —
   is not mechanically checkable)
   *Why:* cancellation is a caller right; the F-23 detach bug is what an
   undocumented Background() looks like.

2.3 Wrap errors with `%w` and a package prefix (`fmt.Errorf("pkg: ...: %w", err)`)
   when callers may inspect the cause. **[lint]** (`errorlint`) **+[reviewer]**
   (sampling for prefix style).

## 3. Concurrency and locking

3.1 `task test:backend` always runs with `-race`; nobody "temporarily" drops
   it. **[gate]** (the Taskfile task itself is the checked artifact)
   *Why:* F-22 (`atomicAdd` that wasn't) shipped invisible until a concurrent
   test ran under the detector.

3.2 Shared mutable state (maps, counters) has one named owner and mechanism
   (mutex or atomics) declared at the field; no lock is held across external
   I/O or callbacks. **[reviewer]** (every new shared field) **+[test]** (the
   `contracttest` suite exercises concurrency under `-race` for engine seams)
   *Why:* names and comments do not establish atomicity — `atomicAdd` proved it.

3.3 Whoever creates a channel closes it exactly once, in the owner goroutine.
   **[test]** (engine event STREAMS are pinned by `contracttest`
   close-exactly-once on done/error/cancel; other channels are
   **[reviewer]**)

3.4 Every launched goroutine has a termination and completion owner (cancel
   path, done channel, or bounded lifetime). **[reviewer]**
   *Why:* the F-24 orphaned-run bug is a goroutine without an owner.

## 4. Testing floors and runtime budgets

4.1 Every package under `internal/`, `chitta/`, and `distiller/` ships at
   least one `_test.go`; that it exercises the public entrypoints is the
   reviewer's + suite's job, not the gate's. Exempt (documented): thin
   `cmd/` mains (via their `AGENTS.md`), `tools/` one-shots, generated
   `apigen`, and the `contracttest` test-infrastructure package (its suite
   runs in consumers' tests).
   **[gate]** (`go list` test-file existence) **+[reviewer]** (entrypoint
   coverage)

4.2 No live network in unit tests: external endpoints only via fakes,
   `httptest`, or injected factories (see the eino adapter's `newModel` seam).
   **[gate]** (literal-URL convenience calls — `http.Get("https://…")`;
   variable-URL calls, `Client.Do`, and other clients are **[reviewer]**)
   *Scope note:* `http.Get(server.URL)` against an httptest server is the
   sanctioned harness pattern.

4.3 The full backend suite has a warm-run wall-clock budget with a machine
   qualifier (measured on this dev box: **≤ 20s** per-package sum; packages
   over 5s are flagged and justified); budgets come from measurement, never
   aspiration. **[gate]** (`task budget:backend` — runs the suite, parses
   timings, fails over budget; reports go version + hostname)
   *Why:* the suite is run after every task; a slow suite is a skipped suite.

4.4 Every defect fix lands with a regression test that fails on the pre-fix
   code, in the same commit. **[reviewer]** (zbot-reviewer verifies the
   failure or equivalent evidence)

4.5 Engine implementations run the shared conformance suite
   (`internal/engine/contracttest`) against fakes AND the real adapter.
   **[test]** (the suite itself)

## 5. Review gates

5.1 Boundary checks are automated in `task verify`: no eino import outside
   `internal/engine/eino/`; no `internal/` import of `cmd/`/`frontend/`/
   `desktop/`. **[gate]**
   *Why:* these five constitution boundaries were hand-run greps until this
   change; hand-run checks rot.

5.2 Every package directory under `backend/` carries an `AGENTS.md`
   (responsibility, boundaries, key entrypoints). **[gate]** (existence)
   **+[reviewer]** (content quality — responsibility/boundaries/entrypoints
   actually present)

5.3 Contract drift gate: `contracts/openapi.yaml` must be unchanged in a
   change that does not declare contract modification (opt-in:
   `CONTRACT_CHANGE=1` with the diff recorded in the change); and the
   generated bindings (apigen + frontend client) must MATCH the contract —
   the gate regenerates via `task codegen` and fails on any delta.
   **[gate]**

5.4 A change that alters any rule encoded here updates this document in the
   same change (constitution: docs move with the code). **[reviewer]**

## Characterization tests as rule enforcement

Where current behavior is buggy but its fix is out of scope (F-15, F-23, F-24,
F-30), the rule is a **characterization test that pins the bug** plus a ledger
entry in the owning follow-up — never a silent comment. When the follow-up
lands, the test flips to assert the fixed semantics. Current pinned-bug tests:
`chat/emit_test.go`, `engine/eino/adapter_test.go` (detach + overlap),
`recalleval` vector-insensitivity.
