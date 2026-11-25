# cmd/zbotd

The Karta daemon entrypoint. Wiring only — all behavior lives in
`internal/*`. Resolves the home (`--home` flag > `ZBOT_HOME` > `~/.zbot`),
bootstraps the control plane, strictly loads `config.yaml`, and serves
the contract on `cfg.Server.Address` (loopback is the default and
recommended bind — the daemon is unauthenticated; a wildcard/LAN bind
is an explicit operator choice that adapts the request guard, see
internal/server/guard.go) with graceful SIGINT/SIGTERM shutdown. Provider
changes: managed files under `config/providers/` (create/edit/delete,
watched) apply immediately; `config.yaml` provider entries apply on
restart. Control-plane registry: loads config/{prompts,skills,hooks,mcps} and ~/.zbot/agents once at startup and hot-reloads on external edits
(`registry.Watch`); Settings MCP/skill management mutates through the
registry's write paths, and the MCP connection probe is wired to
`mcpclient.GoSDKProber`. `version` is stamped via `-ldflags "-X main.version=..."`.

## Agent tool plane

`main.go` composes the tool surface with `tools.Multi(tools.BuiltinProvider{}, chittaSys.ToolProvider(...))` — the builtins (clock, echo) plus the seven chitta memory/knowledge tools — and passes it to both the chat service and the engine. Duplicate tool names panic at composition (startup), never mid-run.

## Session model wiring (researchpath.go)

`researchpath.go` holds the add-chat-modes wiring: the ward binder
(every engine ward enter updates the session row), the research path
(research sessions run the wrapped engine pipeline in their own
transcript, completing as units), and the lean-chat composition (persona
without custom sections + chitta recall as ## Context). Quick chat is
lean (no intent pipeline, no delegation) but carries the WARD plane —
`chatplane.go` mounts `ward_use/ward_create/read/write/list/shell` +
the live MCP tools on one sticky host for the default session, composes
`## Active Ward` after the persona when hosted, and emits
`ward_entered`/`ward_created` (attempted-durable-first) for that
session. Research sessions run the engine's own sticky hosts.

## Intent middleware wiring (intentwiring.go)

`wireIntentMiddleware` (intent-analyst-agent S1) builds the wrapped
engine every goal run uses: the intent-analyst agent's decision with
its execution row (`store.CreateAnalysisExecution` / `FinishExecution`),
the durable-first `analysis_*` event emission, the per-run Settings
read (`intent.ReadSettings`), and the model-derived limits seam
(`analysisWindow`: `catalog.ModelOrID` → `providers.ResolveWindow`).
`catalog.go` restores/persists the model catalog (ids + context
windows) around the wrapper. Disabled by config → the plain engine.

## Intentionally thin (test-floor exemption)

This main is wiring only and carries no tests BY DESIGN (go-rules.md
4.1): every behavior it composes lives in and is tested under
`internal/*`. If logic ever grows here (argument parsing beyond flags,
startup heuristics), move it to an internal package and test it there.
