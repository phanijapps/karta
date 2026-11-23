# internal/progress

The goal-run progress tracker (the operator's agentzero algorithm,
Go): scores each tool call to distinguish productive work from stuck
loops, drives nudges instead of hard stops, and decides when a run
deserves auto-extension.

## Responsibility

- `Tracker.Record(name, args, err)` — observe one tool call; the score
  updates immediately (diversity +1, success +1, exact-repeat fail -5,
  low-diversity failure windows -3/-8).
- `Tracker.Stuck()` — clearly looping (score ≤ -5 after 10+ calls).
- `Tracker.ShouldStop()` — kill ONLY when stuck + warned + sinking.
- `Tracker.ShouldExtend(max)` + `Extend()` — productive agents earn
  more windows (resets the scoring state).
- `Tracker.Nudge(turn, soft, hard)` — the advisory message for this
  step: stuck warning, turn budget wrap-up, failing-call with error
  text, planning nudge.

## Boundaries

- Pure state + scoring: no I/O, no LLM calls, no imports beyond stdlib.
  The engine adapter is the only consumer.

## Key entrypoints

`New`, `Record`, `Tick`, `Stuck`, `ShouldStop`, `ShouldExtend`,
`Extend`, `Nudge`, `PlanningNudge`.
