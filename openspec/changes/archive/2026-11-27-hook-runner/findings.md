# Findings — hook-runner

Reviewers are read-only (diffs written to `/tmp/*.diff`). Severity per the
playbook: P0/P1 fixed before the next phase; P2 fixed or recorded here.

## G1 — phase gate (zbot-reviewer on gpt-6.1-terra, first pass: FAIL → fixed)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| F1 | P1 | PreCompact called the raw runner inside the policy — no attributed `hook_decision`. | **Fixed**: the compaction bridge raises PreCompact through `runHook` before asking the policy (G2 R3 then removed the policy's own hooks — one seam). `TestHookPreCompactDecisionIsEmitted`. |
| F2 | P1 | Terminal hooks used the cancelled run context — an audit/cleanup command never started. | **Fixed** (refined in G2 R1 and G3 R9): execution runs under an uncancellable context, each hook bounded by its own manifest timeout; streaming keeps the cancellable one. `TestHookTerminalHookRunsOnCancellation` (a side-effect file). |
| F3 | P1 | The timeout killed the shell, not its process group. | **Fixed**: the hook runs in its own process group; `cmd.Cancel` kills the group. `TestTimeoutKillsTheWholeProcessGroup` (unix-only file). |
| F4 | P2 | A failed child's advisory reason was discarded with the answer. | **Fixed**: the error result carries `(hook — …)`. `TestHookFailedChildsAdvisoryReachesTheParent`. |
| F5 | P2 | Model-creation / goal-persistence failures ran SessionEnd without a SessionStart. | **Fixed**: SessionStart is the first thing prepareRun does. `TestHookSessionStartThenFailedEnd`. |
| F6 | P2 | The docs example targeted `ward_write`; the tool is `write`; a raw JSON grep is brittle. | **Fixed**: `matcher: ^write$` + a JSON-aware python one-liner, executed before documenting (exit 2 for `secrets/`, 0 otherwise). |
| F7 | P2 | The market-hours example was inverted. | **Fixed**: blocks inside 14:00–21:00 UTC. |
| F8 | P2 | The inherited environment may carry `api_key_env` provider credentials. | **Fixed (docs)**: stated in the operator reference, the runner AGENTS.md and the design's shell-surface risk. |
| F9 | P2 | Tautological child-id assertion; missing PostToolUse-payload, blocked-delegation and legacy-SessionEnd tests. | **Fixed**: all four added; the child decision is compared with the actual child execution. |

## G2 — re-gate (terra): FAIL → fixed

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R1 | P1 | `WithoutCancel` also removed the escape from a full event channel (a vanished consumer → deadlock before `FinishExecution`). | **Fixed**: `runHook` separates EXECUTION (`WithoutCancel` + a 2-minute cap on the raise) from STREAMING (the run's cancellable ctx). |
| R2 | P1 | The docs example fed a heredoc to python's stdin, so it never saw the event JSON, and `exit 0` masked its result. | **Fixed**: a `python3 -c` one-liner as the last command; parsed from the doc and executed (exit 2 / 0) before committing. |
| R3 | P1 | A policy configured with `Hooks` raised a second, un-emitted PreCompact; the Compaction Rule text conflicted. | **Fixed**: `compaction.Service` no longer has hooks — the bridge is the only raise point; `backend/AGENTS.md` Compaction Rule and the package docs say so. |
| R4 | P2 | PostToolUse after a cancelled tool call could not start. | **Fixed** by R1 (every raise executes uncancellable). |
| R5 | P2 | No test for the failed-child advisory. | **Fixed**: added. |
| R6 | P2 | No test for the model-factory failure path. | **Fixed**: added. |
| R7 | P2 | The process-group test used `syscall.Kill` without a build constraint. | **Fixed**: `proc_unix_test.go` (`//go:build !windows`). |

## G3 — third pass (terra): FAIL → fixed

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R8 | P1 | Windows build: the process-attr stub returned `any`. | **Fixed**: `*syscall.SysProcAttr` (nil); `GOOS=windows go vet ./internal/hooks/` clean. |
| R9 | P1 | The 2-minute aggregate raise cap let a slow earlier hook downgrade a later BLOCKING hook to a warning. | **Fixed**: no aggregate cap — each hook is bounded by its own manifest timeout (the raise is bounded by their sum; the roster is the operator's). |
| R10 | P1 | The legacy loop's tool calls bypassed the seam (`execOffered` directly). | **Fixed**: `executeTool` is THE seam for both run paths; `TestHookBlockedLegacyCall`. Spec: "every tool call, on either run path". |
| R11 | P2 | The R6 test did not assert start-before-end. | **Fixed**: the durable order is asserted. |

## G4 — fourth pass (terra): FAIL → fixed

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R12 | P1 | Legacy ROOT runs rebuild history from durable rows, so a PostToolUse block never reached the next model request (the spine's state carries the replacement; children did). | **Fixed**: the legacy root overlays post-blocked results by call id (`blockedResults` → `rowsToMessagesOverlay`); `TestHookPostBlockOnTheLegacyRoot`. |
| R13 | P2 | Suppressed spine calls bypassed the seam (the docs promise PreToolUse for refused calls too). | **Fixed**: the suppression refusal runs through `executeTool`. |
| R14 | P2 | Stale record text (`hookRaiseTimeout`, the policy "keeps Hooks", the cap). | **Fixed** in engine/eino AGENTS.md, design.md, findings F2. |

## G5 — fifth pass (terra, DoD): NOT READY → fixed

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R15 | P2 | The legacy overlay was keyed by call id only — a model reusing an id within a run could get an earlier block applied to a later allowed result (or the reverse). | **Fixed**: the overlay records what the model saw per tool-call OCCURRENCE (`modelFacing[callID] = []string`, applied in order); `TestHookOverlayIsKeyedByOccurrence` (two calls sharing an id: the first allowed, the second blocked — each keeps its own verdict; both rows keep the real output). |
| R16 | P2 | Stale comments: mw_compaction.go (PreCompact "inside the policy", "Hooks unset"); hooks/AGENTS.md promised group kills without the Windows exception. | **Fixed**. |
| — | incomplete | Closure (task 3.3: `task verify`, the live check, archive + audit) outstanding. | Done in this commit sequence (see signoff). |

## Live check — a manifest blocking one tool on the real stack (2026-11-28)

A temporary `~/.zbot/hooks/karta-live-check.yaml` (`event: PreToolUse`,
`matcher: ^clock\.now$`, `command: echo 'clock is retired for this check'
>&2; exit 2`) — hot-loaded by the registry, no restart — then the browser
script with the goal "Call the clock tool once and tell me the exact UTC time
it returns … If the tool fails, tell me exactly what it said." Session
`research-1795835228372361665-1`, 24 s, run completed, 0 console errors.

- The `clock.now` row: `error: blocked by hook: karta-live-check: clock is
  retired for this check` — no timestamp: the tool was never executed; the
  model's answer quotes the reason verbatim ("`clock.now` failed. Exact
  output, verbatim: …") and moves on.
- Durable `hook_decision` rows (timeline_events), in order: SessionStart
  allow · PreToolUse/PostToolUse allow for ward_use, read, shell · **PreToolUse
  clock.now: block — karta-live-check: clock is retired for this check** ·
  SessionEnd allow — every decision on the record, attributed.
- The manifest was removed afterwards; `~/.zbot/hooks/` is empty again.
