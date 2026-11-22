# scheduler

Cron-fired goal runs: evaluates the registry's schedule definitions
(config/schedules) on a one-second tick and starts `RunGoal` into each
schedule's persistent session (`sched-<name>`), so a schedule's whole
history lives in one Timeline.

- Fire semantics: one in-flight run per schedule — a fire while the
  previous run is active is skipped and recorded; missed fires follow
  the schedule's policy (`skip` default forgets; `catch_up` fires once
  on wake). Every decision emits to the eventbus (no silent work).
- `ValidExpression` is the single parse check shared by the runtime and
  the API's create/update validation (typed 400s).
- Runs start under `context.WithoutCancel` of the tick ctx so a
  controller restart doesn't kill in-flight scheduled runs mid-goal.
- Imports registry + domain + eventbus only; the engine arrives as the
  `Engine` seam (no eino here).
