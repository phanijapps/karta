# Proposal

## Why

Adding an MCP server today means an inline form with a confusing
transport checkbox that shows the URL field even in stdio-only mode.
Real-world MCP configuration lives as standard `mcpServers` JSON
(modelcontextprotocol.io quickstart, Claude Desktop, Cursor, VS Code) —
users copy those blocks and should be able to paste them, not
re-type fields. Both flows deserve a proper dialog.

## What Changes

- **Add/Edit MCP becomes a dialog** (the Providers modal pattern), not
  an inline form, with two modes chosen by a segmented control:
  **Form** and **Paste JSON**.
- **The transport toggle is replaced by a segmented transport
  selector** — Local process (stdio) | Remote URL (http) | Both — that
  swaps the visible fields. URL fields appear only for Remote/Both;
  command fields only for Local/Both. No permanently-visible
  irrelevant fields.
- **Standard JSON import** via `POST /api/mcps/import` — the paste
  travels as RAW TEXT and the daemon is the only parser (order,
  number literals, and duplicate keys survive exactly; duplicates
  reject the whole document before any write). Accepted
  shapes: the official `{"mcpServers": {...}}` wrapper, the VS Code
  `{"servers": {...}}` alias, a bare `{name: server}` map, or a single
  server object (the dialog's name field supplies the key). Each
  accepted server lands as its own independent managed manifest under
  `config/mcps/<slug>.yaml` — the import is per-server independent
  (never transactional), reusing the existing create rules (slug from
  the JSON key, managed-name validation, capacity, no-clobber).
- **Import tolerance is explicit and reported, never silent.** Unknown
  or unsupported keys (`headers`, `cwd`, `autoApprove`, `alwaysAllow`,
  comments) are ignored with a per-server warning naming them;
  `type: "sse"` imports as the HTTP url with a deprecation warning
  (Test connection verifies it); `disabled: true` entries are skipped
  with that reason; `os`-scoped entries not matching this platform are
  skipped with that reason; env values that are JSON numbers or
  booleans are coerced to strings with a warning (objects, arrays, and
  null fail that server). Every server in one paste gets its own
  outcome row: created / skipped (duplicate, disabled, other-OS) /
  failed (shape, field, name, capacity, `io_error`) with the specific
  reason — a write failure on one entry never aborts the rest.
- **Edit round-trips through JSON too**: the edit dialog's Paste JSON
  mode prefills the server's current configuration as a standard
  `mcpServers` block and saves through the import endpoint's replace
  mode (exactly one entry, the server's transports replaced wholesale,
  name unchanged — renames stay in Form mode), so awkward values
  (multiline env, newline-bearing args) edit as JSON with full fidelity.

- **Server detail is a popup.** Selecting a server in the list opens a
  dialog with its transports, an enable/disable switch, and the actions
  (Test connection, Edit, Delete) — the inline detail card is gone.
- **Enable/disable.** MCP manifests carry `enabled` (absent = enabled);
  toggling persists to the manifest file and applies immediately.
  Disabled servers stay registered, listed, toggleable, and probeable —
  future runtime wiring skips them; the connection test still verifies
  their configuration.

**Out of scope:** bulk export of the registry as `mcpServers` JSON;
importing other tools' full config files (only the mcpServers map is
read); SSE transport support (SSE urls import as http and the probe
reports honestly); headers/auth on HTTP transports (contract has
none — warned and dropped); skills UI changes; runtime wiring.

## Capabilities

### New Capabilities

- `mcp-import`: Accepting standard `mcpServers` JSON as raw text (and its common
  real-world variants), validating per server, mapping each entry to a
  managed manifest, and reporting per-server outcomes with explicit
  tolerance rules (ignored keys, coerced env, skipped disabled/other-OS
  entries).

### Modified Capabilities

- `api-contract`: Adds `POST /api/mcps/import` — per-server result
  arrays on 200; typed 400s for malformed JSON, non-object shapes,
  empty pastes, and oversized payloads.

- `mcp-management` (new requirement): the `enabled` manifest field and
  its semantics.
- `api-contract` (new requirement): the `enabled` field on MCP
  responses and create/edit inputs.
- `settings-shell`: MCP add/edit moves into a dialog with Form and
  Paste JSON modes, the segmented transport selector replaces the
  toggle, and import results render per server.

## Impact

- **Backend:** `internal/server` (import endpoint, raw-text parser,
  field-policy mapper, replace mode), `internal/registry` (exports
  `Slugify` as the one key normalizer; create/update rules otherwise
  unchanged).
- **Contract & codegen:** one additive endpoint + result schemas.
- **Frontend:** MCP dialog (two modes, segmented transport control,
  per-server results panel), client method, Playwright flows.
- **Docs:** frontend README, mcp-management notes in evidence.
