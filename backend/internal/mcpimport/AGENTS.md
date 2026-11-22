# mcpimport

The MCP paste-import parser: turns a pasted JSON document (official
quickstart format or a single raw entry) into validated
`registry.MCPSpec` candidates with per-entry actions (skip/fail/warn),
applying the normative field-policy table (only command/args/env/url
reach the spec; auth is never trusted).

- Consumed by the server's import handlers (HTTP edge); this package is
  pure parsing/validation — no HTTP, no I/O, no daemon state.
- `DocError` is the whole-document rejection (typed 400, nothing
  created); per-entry outcomes carry the reason vocabulary
  (`ReasonDuplicate`, `ReasonOtherOS`, ...).
- Parser tests live here; handler/marketplace/guard tests live in
  `internal/server` (they exercise the HTTP contract).
- Imports registry only (plus stdlib); never imports server or client
  code.
