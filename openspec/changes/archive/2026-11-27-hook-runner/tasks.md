# Tasks

## 1. The runner

- [x] 1.1 `internal/hooks/runner.go`: selection (event + subject matcher,
  key order), `sh -c` execution in the manifest's directory with the event
  JSON on stdin, bounded output, the timeout clamp, the exit-code contract,
  combining (block > warn > allow), `Context.hooks`. `AGENTS.md`. Verify:
  table-driven tests with `sh -c` commands (0/2/3, stderr reasons, a
  timeout, stdin inspection, matcher selection, combining, a missing
  binary → warn).

## 2. Engine seams

- [x] 2.1 `SetHookRunner`; `runHook` (run + emit `hook_decision`, durable-first,
  attributed; child decisions as delegation-kind on the stream). Verify:
  the decision event is durable, published and streamed; an allow is on the
  record.
- [x] 2.2 Pre/PostToolUse in the tool wrapper (live plane, dispatch alias,
  foreign tools): block → not executed / model-facing replaced, durable row
  keeps the output. Verify: a blocked tool never executes; a post-block hides
  the output from the model only.
- [x] 2.3 SessionStart (prepareRun, roots) and SessionEnd (`finishRun` at
  every root terminal on both loops). Verify: fired once each with the status.
- [x] 2.4 SubagentStop on the fold-back (advisory append). Verify: the root
  reads the hook's reason beside the report; the decision attributed to the
  child.
- [x] 2.5 Wiring: the daemon passes the runner to the engine; the
  compaction BRIDGE raises PreCompact through it (the policy carries no
  hooks — one emitting seam, gate R3). Verify:
  `TestHookPreCompactDecisionIsEmitted` (a blocking manifest is an
  attributed, durable decision and skips the pass).

## 3. Docs, gates, archive

- [x] 3.1 `backend/AGENTS.md` hook rule → the contract as built; engine +
  hooks AGENTS.md; a commented example manifest in the docs.
- [x] 3.2 Phase gate (gpt-6.1-terra); fix P0/P1; P2s in findings.md.
- [x] 3.3 `task verify` green (dev stack stopped); a live check: a manifest
  blocking one tool on a real run (findings.md "Live check"); archive
  with the scenario audit (signoff.md).
