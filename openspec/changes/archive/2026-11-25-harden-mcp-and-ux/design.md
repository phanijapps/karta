# Design

## Context

The MCP execution bridge (ward-v2) runs stdio servers live but with
the probe's discipline only: one 60s call timeout, no failure
taxonomy, no restart backoff, and `exec.Command(spec.Command)` with a
bare name — `npx` failed for the operator until the config carried an
absolute path. Three UX P2s ride along: streaming /v1, leaving a
ward, and research auto-titling.

## MCP hardening

```
binary resolution   resolveBinary(cmd): PATH + ~/.local/node*/bin,
                    ~/go/bin, ~/.cargo/bin, ~/.bun/bin, ~/.local/bin,
                    pnpm dirs — the daemon's env is thinner than a
                    shell's; configs written for a shell must work.
failure taxonomy    typed *mcpFailure{Kind, Server, Tool, Err}:
                    timeout | unavailable | protocol | server_error
                    | bad_arguments — the engine maps Kind to the
                    Timeline text ("mcp brave: timeout after 60s").
per-call bounds     callTimeout 60s (unchanged default), now typed.
restart backoff     consecutive start failures per server → backoff
                    2s·2^n capped at 2min; a healthy start resets.
                    Refresh skips a server inside its backoff window.
events              every call emits through the engine's tool_call
                    path already; the Result text carries the taxonomy
                    so the Timeline shows WHY a call failed.
```

The executor imports only registry + tools + the go-sdk (no engine) —
the engine keeps mapping results to events.

## /v1 streaming

`stream: true` → `text/event-stream` of OpenAI chunks:
```
data: {"id":…,"object":"chat.completion.chunk","created":…,"model":…,
       "choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}
data: {…"delta":{"content":"…"}}…               (chat: real deltas)
data: {…"delta":{"content":"🔧 mcp_brave…\n"}}…  (research: work frames)
data: {…"delta":{},"finish_reason":"stop"}…
data: [DONE]
```
- Chat sessions: the lean stream's deltas map 1:1 to content chunks.
- Research sessions: the Timeline SSE (already live for the UI) maps
  to content chunks prefixed by a marker (`🔧` tool, `👥` delegation,
  `🧠` step) so OpenAI clients watch the run; the final assistant row
  lands as the last content chunk before `finish_reason: stop`.
- `stream: false` / absent → today's JSON. Errors mid-stream are a
  final chunk with `finish_reason: "error"` + the message (OpenAI
  clients tolerate it; no spec'd error frame exists).

## Leave ward

`POST /api/sessions/{id}/leave-ward` → 204. Clears the row's ward
binding; the engine host for that session drops its ward (new
`Adapter.DropWard(sessionID)`); the session stays research and
re-groups under Research (a shell again). Refuses 409 on the default
chat session. UI: the composer's ward strip gains **Leave**.

## Research auto-titling

The wrapper's post-analysis hook: when the session has no title and
the analysis produced a `primary_intent`, `store.SetSessionTitle(id,
slug(primary_intent, 64))`. Runs once per session (title present →
no-op); user titles never overwritten. The sidebar already renders
titles; the Research group's rows become meaningful without a rename
surface.

## Decisions

1. **Work frames as chunk content** (not a custom event): OpenAI
   clients only render `delta.content`; a custom field would be
   dropped. Prefix markers keep them legible.
2. **Binary resolution is additive**: the configured command is tried
   as-is first; only a miss triggers the directory sweep.
3. **Leave drops the host ward too** — otherwise the next run would
   pre-load the ward the user just left (EnsureWard re-entry).
4. **Slug titles are English-ASCII** (spaced words, lower-cased, ≤ 64
   chars); an intent with no ASCII letters yields no title (the
   session stays "Research") rather than a garbage label.

## Risks / Trade-offs

- [Stream + busy run] — a research stream on a busy session returns
  the typed 409 as JSON before the stream opens (same as non-stream).
- [Backoff hides a fixed server] — a server that starts failing then
  gets fixed waits ≤ its backoff (≤2min) or the next registry change.
- [Title slug collisions] — two sessions may share a title; ids stay
  unique, the sidebar shows recency — acceptable.

## Migration Plan

None: additive endpoints/fields; MCP behavior changes are strictly
more informative (typed failures) and more forgiving (paths).

## Open Questions

- Should Leave also clear the session's Active Ward *packet* from the
  transcript history? No — history is the truth; the strip reflects
  the binding, not past runs.
