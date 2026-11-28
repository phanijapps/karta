# Hooks — the operator reference

Hooks are the operator's own lifecycle scripts under `~/.zbot/hooks/*.yaml`.
Karta loads them at startup (and on edit — the registry hot-reloads) and runs
them at the moments below. A hook cannot fail a run: it can only **allow**,
**block** (the model sees your reason and recovers) or **warn** (the reason
lands on the Timeline; the run continues).

## Manifest

```yaml
event: PreToolUse        # PreToolUse | PostToolUse | PreCompact | SessionStart | SessionEnd | SubagentStop
matcher: ^write$         # optional regex; the tool name (Pre/PostToolUse) or the agent name (SubagentStop); ignored otherwise
command: >-              # required; runs through `sh -c` in this file's directory; stdin = the event JSON
  python3 -c 'import json,sys; ev=json.load(sys.stdin); p=ev.get("tool_input",{}).get("path","").lstrip("./");
  bad=p.startswith("secrets/"); print("secrets/ is read-only", file=sys.stderr) if bad else None;
  sys.exit(2 if bad else 0)'
timeout_ms: 5000         # optional; default 10000, clamped to 60000
```

The command receives the event as JSON on **stdin**:

```json
{"event":"PreToolUse","session_id":"research-…","tool_name":"write","tool_input":{"path":"secrets/keys.md","content":"…"}}
```

Tool names are the ones the engine offers: the ward tools are `read`,
`write`, `list`, `shell`, `ward_use`, `ward_create`; the delegation tools
are `delegate_<agent>`; `matcher` selects by name only — a path or content
policy is the script's job (parse the JSON; a raw `grep` on it is brittle).

(`tool_input` is the parsed call; `payload` carries the subject for the other
events — the output for PostToolUse, the goal for SessionStart, the status
for SessionEnd, the agent/status/execution for SubagentStop.)

## Exit-code contract

| Exit | Decision | What the run does |
|---|---|---|
| 0 | allow | proceeds; the decision is still on the Timeline |
| 2 | **block** | PreToolUse: the call is **not executed**, the model sees `error: blocked by hook: <stderr>`; PostToolUse: the model sees `<stderr>` instead of the output (the durable row keeps the real output); PreCompact: the pass is skipped; Session*/SubagentStop: read as a warning |
| other | warn | proceeds; stderr is the reason on the Timeline (and, for SubagentStop, appended to the report the root reads) |
| timeout | warn | killed at `timeout_ms`; "timed out after …" |

Several hooks on one event run in file-name order and combine: block wins,
then warn; the Timeline text names each hook (`no-compact: not during market
hours`). Output is bounded (16 KB per stream, 2 KB of reason).

## Examples

Block ward writes that touch `secrets/`; warn (don't stop) when a child
reports; keep compaction out of market hours:

```yaml
# ~/.zbot/hooks/ward-secrets.yaml — the manifest above, verbatim
```
```yaml
# ~/.zbot/hooks/verify-reports.yaml
event: SubagentStop
matcher: ^research
command: echo 'verify the capacity figures against two sources' >&2; exit 3
```
```yaml
# ~/.zbot/hooks/no-compaction-in-hours.yaml — block compaction inside market hours (14:00–21:00 UTC)
event: PreCompact
command: h=$(date -u +%H); [ "$h" -ge 14 ] && [ "$h" -lt 21 ] && { echo 'not during market hours (14–21 UTC)' >&2; exit 2; }; exit 0
```

## Where decisions show up

Every decision is a `hook_decision` Timeline event (`GET /api/sessions/{id}/events`;
a Work-tab line is a follow-up): `hook PreToolUse write: block —
ward-secrets: secrets/ is read-only`. Hooks are local-first by construction:
they run on your machine, as your user, **with the daemon's environment** —
if a provider credential is configured through `api_key_env`, a hook can
read it, exactly like your agents' prompts and the ward `shell` tool can.
The same trust level applies to all three; keep secrets out of the
environment on a machine that runs hooks you did not write.
