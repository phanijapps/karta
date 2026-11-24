# mcpclient

Settings-plane MCP probe: verifies a configured MCP server by running a
real client handshake (initialize → tools/list → shutdown) per declared
transport — stdio (`CommandTransport`, manifest env passed to the child)
and/or streamable-http (`StreamableClientTransport`) — concurrently under
one shared deadline with a straggler grace, over the official
`github.com/modelcontextprotocol/go-sdk` (v1.8.0, pinned by the
manage-mcps-and-skills preflight; `mark3labs/mcp-go` is the recorded
fallback behind the seam).

- Probes never execute tools, never mutate registry state, and never
  touch the platform keyring. Failure reasons are the stable contract
  enum: `spawn_failed` (missing binary / fork-exec), `connect_failed`
  (dial refused), `timeout` (deadline; stragglers past the envelope are
  abandoned with a synthesized timeout), `protocol_error` (everything
  the SDK reports as protocol-level).
- Tool listings are capped (256) with a `truncated` flag; ListTools
  pagination is followed up to the cap; externally sourced tool names
  and descriptions are byte-clamped (128/1024) to the contract's
  response bounds without splitting runes.
- Boundaries: imports `registry` (only the `MCPSpec` type), stdlib, and
  the SDK. Sits beside `tools`, below `server`; never imports
  `engine/*` (the eino boundary — this is not engine wiring), never
  imports config/providers.
- Key entrypoints: `Prober` (the seam server handlers take; fakes in
  tests), `GoSDKProber.Test`, `ProbeResult` / `TransportResult` /
  `Failure`. Tests build a real stdio server from `testdata/echoServer`
  and serve loopback-only HTTP endpoints; zero external network.

## Execution bridge (ward-v2)

`Executor` (executor.go) keeps configured stdio MCP servers connected
and executes their tools live: Refresh reconciles the roster (registry
snapshot), Tools() adapts them to the tool plane (mcp_<server>_<tool>,
sanitized), Call routes with re-dial after a dead session. Fail-soft:
a server that cannot start just has absent tools + a log line.

## Hardening (harden-mcp-and-ux)

`resolve.go`: `resolveBinary` (PATH → HOME tool dirs; versioned node
dirs discovered newest-first; verbatim fallback) and `backoffFor`
(2s·2^n, cap 2min). `ExecFailure` is the typed execution failure
(timeout / unavailable / protocol / server_error / bad_arguments) the
engine maps to Timeline text. A dead server entry is KEPT across
retries so the backoff grows; a spec change resets it; Exec-side
redials honor the window; stale tools on deconfigured servers return
typed unavailable (never a nil-session panic).
