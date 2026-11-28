# hooks
The hook runner — `domain.HookRunner` made real over the registry's hook
manifests (`~/.zbot/hooks/*.yaml`), by the backend constitution's
exit-code contract.
- `Runner.Run(ctx, HookEvent)`: selects the roster's hooks for the event
  (key order) whose `matcher` matches the event's SUBJECT (`Subject`: tool
  name for PreToolUse/PostToolUse, agent name for SubagentStop; ignored
  for subject-less events), runs each `command` through `sh -c` in the
  manifest's directory with the event as JSON on stdin, and maps the
  outcome: exit 0 allow, exit 2 block (stderr = the reason), any other
  non-zero or the timeout (manifest `timeout_ms`, default 10 s, clamped
  1–60 s) warn-and-continue. Decisions combine block > warn > allow with
  per-hook reasons (bounded 2 KB); `Context.hooks` names what ran.
- A hook failure is a DECISION, never an error — a run must not fail
  because a hook did. `Run` errors only on a missing roster.
- Process tree: on Unix the hook runs in its own process group
  (`proc_unix.go`) and a timeout or a cancelled parent kills the WHOLE
  group (a compound command's children included); on Windows only the
  shell itself is killed (`proc_windows.go`). The environment is inherited — including
  any `api_key_env` provider credential (operator trust; see
  docs/development/hooks.md).
- Kernel-pure: imports domain + registry; no eventbus, no store — the
  engine seams emit the `hook_decision` events (constitution rule 4).
- Tests (`runner_test.go`) drive real `sh -c` commands: the three exit
  codes, a timeout, stdin inspection, matcher subjects, combining, a
  missing binary, bounded output.
