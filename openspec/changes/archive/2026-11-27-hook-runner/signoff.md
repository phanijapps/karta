# Signoff — hook-runner

**Change:** `hook-runner` — `internal/hooks` implementing `domain.HookRunner`
by the constitution's exit-code contract, wired into every engine seam.
**Branch:** `feature/provider-plane-and-model-catalog`. **Date:** 2026-11-28.
**Status:** complete, gated (G1–G5), verified, live-checked; archive on the
operator's "go" (the playbook's archive step).

## 1. What shipped

- **The runner** (`internal/hooks`): `sh -c` in the manifest's directory, the
  event JSON on stdin, exit 0 allow / 2 block (stderr = the reason) / other
  non-zero or timeout warn; per-hook manifest timeouts (default 10 s, clamped
  1–60 s); block > warn > allow in key order with per-hook reasons; the
  matcher selects by subject (tool name / agent name; ignored otherwise);
  bounded output; process-group kill on Unix; a hook failure is a decision,
  never a run failure.
- **The seams** (`engine/eino/hooks.go`): THE tool-call seam `executeTool`
  (PreToolUse → call → record + durable row → PostToolUse) used by the
  spine's wrapper AND the legacy loop; SessionStart/End at the root run's
  edges (uncancellable execution, cancellable streaming); SubagentStop on
  the fold-back (advisory, also on a failed child); PreCompact raised by the
  compaction bridge (the policy carries no hooks — one emitting seam).
- **Observability**: every decision is a durable-first `hook_decision` event
  with `context.hooks` and the event payload, attributed; a child's decisions
  surface as delegation-kind events. No contract change.
- **Docs**: `backend/AGENTS.md` hook + compaction rules; the operator
  reference `docs/development/hooks.md` (manifest, stdin, exit table, three
  examples — the security example executed before documenting); package
  AGENTS.md (hooks, compaction, engine/eino).

## 2. Gate history (zbot-reviewer on gpt-6.1-terra; findings.md G1–G5)

| Gate | Verdict → outcome |
|---|---|
| G1 phase | FAIL (3 P1: PreCompact un-emitted; terminal hooks on a cancelled ctx; no process-group kill) + 6 P2 → all fixed |
| G2 re-gate | FAIL (3 P1: full-channel deadlock risk; the doc example's heredoc; a second un-emitted PreCompact path) + 4 P2 → fixed (one seam; execution vs streaming contexts) |
| G3 third | FAIL (3 P1: Windows build; the aggregate cap downgrading a later block; legacy loop bypass) + 1 P2 → fixed |
| G4 fourth | FAIL (1 P1: the legacy root ignored a PostToolUse block) + 2 P2 → fixed |
| G5 DoD | NOT READY (2 P2: reused call ids; stale comments) + closure → fixed; closure below |

## 3. Test evidence

`task verify`, dev stack stopped, final tree — `/tmp/task-verify-hook-runner.log`:
**exit 0 in 136 s at 2026-11-28T03:10:37Z**: backend gates OK · codegen no
drift · lint 0 issues · **30 backend packages ok under -race** (incl.
`internal/hooks`) · budget OK · real-LadybugDB suite ok · build ✓ ·
svelte-check 0 errors · **109 e2e passed**. `GOOS=windows go vet` clean for
hooks + engine/eino. Runner tests drive real `sh -c` commands (exit codes,
timeout + process group, stdin, matcher subjects, combining, a missing
binary, bounded output); seam tests use the real runner over manifests in a
temp dir (pre-block never executes; post-block hides from the model only —
spine and legacy root, keyed by occurrence; session edges incl. a cancelled
run with a side-effect file; SubagentStop advisory incl. a failed child;
PreCompact decision emitted; a blocked delegation starts no child; legacy
blocked call).

## 4. Live evidence (real stack, 2026-11-28)

A temporary manifest blocking `clock.now` (hot-loaded, no restart); the
browser script's run (session `research-1795835228372361665-1`, 24 s): the
`clock.now` row = `error: blocked by hook: karta-live-check: clock is retired
for this check` (never executed), the model quoted it verbatim; durable
`hook_decision` rows in order: SessionStart allow · Pre/Post allow ×3 tools ·
**PreToolUse clock.now: block — karta-live-check: …** · SessionEnd allow.
Manifest removed afterwards (findings.md "Live check").

## 5. Decisions (design.md H1–H4, defaults taken)

H1 `sh -c` (the environment is inherited — incl. any `api_key_env`
credential; documented) · H2 a PostToolUse block replaces the model-facing
result only · H3 cwd = the manifest's directory · H4 timeout default 10 s,
clamped 1–60 s. Operator review welcome; none blocks the archive.

## 6. Signoff

Implementer: pi — 2026-11-28. Final gate: zbot-reviewer on gpt-6.1-terra
(G5) — fixes verified; closure recorded here.

Operator: ______________________  Date: __________
