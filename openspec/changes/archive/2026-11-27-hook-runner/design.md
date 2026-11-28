# Design

## Context

What exists: manifests (`registry.HookSpec{Event, Matcher, RawMatcher,
Command, TimeoutMS, Path}`, `Snapshot.Hooks` keyed by file stem; strict
decode; six events); the seam (`domain.HookRunner.Run(ctx, HookEvent)
(HookDecision, error)`; `HookEvent{Event, SessionID, ToolName, ToolInput,
Payload}`; `HookDecision{Action allow|block|warn, Reason, Context}`); the
timeline kind `hook_decision` with `TimelineEvent.Hook *HookDecision` and the
contract schema (`action`, `reason` ≤ 16k, `context` map); the compaction
policy's `Hooks` field (PreCompact, block skips the pass). What does not:
anything that runs a command. The constitution states the contract: "0
allow, 2 block (stderr → agent as recoverable error), other non-zero
warn-and-continue".

## Goals / Non-Goals

Goals: one runner every seam uses; the exit-code contract exactly; every
decision observable and attributed; no run ever fails because a hook did.
Non-Goals: UI rendering of decisions (the kind is subscribed; follow-up),
new events, parallel hook execution (sequential, deterministic), hook
output feeding the model beyond the reason (block/warn text only).

## The runner — `internal/hooks`

```
Runner{Snapshot func() registry.Snapshot; DefaultTimeout time.Duration; MaxOutput int}
Run(ctx, ev) (HookDecision, error)
```

| Concern | Rule |
|---|---|
| Selection | `Snapshot().Hooks` with `Event == ev.Event`, in key order; `Matcher` (when set) must match the event's SUBJECT — `ToolName` for PreToolUse/PostToolUse, `Payload["agent"]` for SubagentStop; for PreCompact/SessionStart/SessionEnd a matcher is IGNORED (no subject; the loader is unchanged) |
| Execution | `sh -c <command>` (pipes and quotes work), cwd = the manifest's directory, stdin = the `HookEvent` as JSON, stdout/stderr captured and bounded (`MaxOutput`, default 16 KB each), env inherited |
| Timeout | `timeout_ms` from the manifest, else `DefaultTimeout` (10 s); clamped to [1 s, 60 s]; a timeout kills the process and is a WARN ("hook timed out after N ms") |
| Exit codes | 0 → allow; 2 → BLOCK, reason = stderr (trimmed; empty → "blocked by hook <key>"); any other non-zero → WARN, reason = stderr or "exit N"; a command that cannot start (sh exits 127/126) → WARN |
| Combining | block > warn > allow; `Reason` = the non-empty per-hook reasons joined with "; " as `<key>: <reason>` (bounded, 2 KB); `Context["hooks"]` = the keys that ran, `Context["event"]` = the event name, `Context["subject"]` = the subject when present |
| Errors | `Run` returns an error only when it cannot read the roster (nil snapshot); hook failures are decisions, never errors — a run must not fail because a hook did |
| Purity | imports `domain` + `registry` only; no eventbus, no store — callers emit |

## The seams — `internal/engine/eino` (behind `SetHookRunner`, nil = all allow)

One helper, `runHook(ctx, events, in, ev) domain.HookDecision`, runs the
roster and emits the decision (see Observability); every seam calls it.

| Event | Fires | Semantics |
|---|---|---|
| PreToolUse | in THE tool-call seam (`executeTool`, hooks.go) before ANY execution — used by the spine's tool wrapper AND the legacy loop; the live plane, the dispatch alias and the foreign (skill) path alike; `ToolInput` = the parsed arguments | block → NOT executed; the model sees `error: blocked by hook: <reason>` as the tool result; recorded and emitted like a refusal (a `tool_call` row/event with the text). warn → proceed. |
| PostToolUse | after execution, `Payload["output"]` = the first 4 KB of the result | block → the MODEL-FACING result becomes `error: blocked by hook: <reason>`; the durable row keeps the real output. warn → proceed. |
| SessionStart | `prepareRun`, root runs only; `Payload{goal}` (first 120 chars) | advisory: block reads as warn |
| SessionEnd | every root terminal (completed / cancelled / failed / stuck / step bound) via one `finishRun` wrapper around `FinishExecution`; `Payload{status, tokens_in, tokens_out}` | advisory |
| SubagentStop | `delegateTool.Exec` after the child returns, before the fold-back; `Payload{agent, status, execution_id}` | advisory: block/warn → `Hook (<keys>): <reason>` appended to the fold-back |
| PreCompact | the compaction BRIDGE raises it through `runHook` (emitted with the run's attribution) before asking the policy; block skips the pass. The policy carries NO hooks (one emitting seam — G2 R3) | |

Legacy loop: every seam fires there too — it runs its tool calls through
the same `executeTool` (a configured block protects both paths while both
exist).

## Observability (constitution rule 4)

Every decision — allow included — is one durable-first `hook_decision`
event: `AppendEvent` → `Publish` → stream, with the run's attribution
(`Agent`, `ExecutionID`), `Text` = `hook <Event>[ <subject>]: <action>[ —
<reason>]`, `Hook` = the decision with `Context.hooks` naming the hooks
that ran. A child's decisions surface in the parent's stream as
delegation-kind events (the durable kind stays `hook_decision`), like its
other steps. No contract change: the schema is used as is.

## Risks

1. **Latency**: hooks run inline; a slow hook slows every tool call — the
   timeout bounds it (default 10 s); manifests set `timeout_ms`.
2. **Shell surface**: `sh -c` runs whatever the manifest says — it is the
   operator's own `~/.zbot/hooks/`, the same trust level as `~/.zbot/agents`
   prompts and the ward `shell` tool. The environment is inherited,
   including any `api_key_env` provider credential (documented in the
   operator reference; gate F8).
3. **Blocking loops**: a PostToolUse block replaces the model-facing result;
   a model that retries the same call re-triggers PreToolUse — the stuck
   tracker and the step bound bound it (same as any failing call).
4. **Output size**: bounded at 16 KB per stream and 2 KB per reason.
5. **Terminal hooks on a cancelled run** (gate F2): the roster EXECUTES
   under `context.WithoutCancel` — an audit or cleanup command must run
   even when the run was cancelled; each hook's own manifest timeout bounds
   it (no aggregate cap: a slow earlier hook must never downgrade a later
   blocking one to a warning — G3). STREAMING keeps the run's cancellable
   context (a vanished consumer can never wedge a full channel — G2 R1).
7. **Process groups** (gate F3): the hook runs in its own process group
   and a timeout kills the group, so a compound command's children do not
   outlive it.

## Decisions (defaults taken; say otherwise to change)

- **H1 — shell**: `sh -c` (commands may use pipes/quotes) rather than
  direct `exec` of a binary.
- **H2 — PostToolUse block**: the model-facing result is replaced by the
  reason; the durable row keeps the real output.
- **H3 — cwd**: the manifest's directory (`~/.zbot/hooks/`), so relative
  script paths in a command resolve beside their manifest.
- **H4 — timeout**: default 10 s, clamped to [1 s, 60 s].
