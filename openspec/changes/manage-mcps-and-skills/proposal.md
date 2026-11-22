# Proposal

## Why

The registry already loads MCP manifests and skills from
`~/.zbot/config/{mcps,skills}/` with hot-reload, but none of it is
reachable: Settings shows mock fixtures ("Illustrative... No process is
started"), the contract has no registry endpoints, and configuring a
tool server means hand-writing YAML with no way to verify it works.
Providers became manageable last change; MCPs and skills are the next
settings plane and the config tree for both already exists.

## What Changes

- **Registry write paths.** `internal/registry` gains CRUD for its two
  managed kinds — `Create/Update/Delete` for MCP manifests and skills,
  serialized with watcher reloads under the existing reload mutex so an
  external edit can never publish a stale snapshot over a completed
  mutation. Writes are atomic (temp-file + rename), validate against the
  load rules, and publish the new snapshot synchronously before the API
  responds. Load errors become snapshot-reachable so listings can show
  hand-edited breakage per file.
- **MCP CRUD over the contract.** `GET/POST /api/mcps` and
  `PATCH/DELETE /api/mcps/{name}`. An MCP's name is its file stem. The
  manifest rule broadens from transport-exclusive to at-least-one:
  stdio (`command/args/env`) and streamable-http (`url`) may each be
  declared alone or together — a server reachable both ways is one
  entry with two transports; only a manifest declaring neither is
  invalid (the registry loader's XOR rejection is superseded). Rename
  is a file swap. Mutations apply immediately via the registry — no
  restart. Creates are capacity-bounded; renames and creates conflict
  on occupied target paths (no-clobber, snapshot-blind races closed);
  failed mutations never leave the daemon's view diverged from disk.
- **MCP connection test.** `POST /api/mcps/{name}/test` runs a real MCP
  client probe over the official `modelcontextprotocol/go-sdk`
  (`CommandTransport` for stdio, `StreamableClientTransport` for HTTP):
  initialize → `tools/list` → shutdown, under one shared deadline,
  once per declared transport (concurrently when both are declared).
  Returns one result per transport — the discovered tool names and
  descriptions, or a typed failure (spawn failed, connect/timeout,
  protocol error). The probe lives in a new `internal/mcpclient`
  package — settings verification only, no runtime agent wiring.
- **Skill CRUD over the contract.** `GET /api/skills`,
  `GET /api/skills/{name}` (detail including the SKILL.md body),
  `POST /api/skills` (create directory + SKILL.md),
  `PATCH /api/skills/{name}` (edit description/body; rename moves the
  directory), `DELETE /api/skills/{name}` (removes the whole skill
  directory including bundles — the UI confirms first). Directory name
  is the slugified frontmatter name, enforced on create and rename.
- **Settings → MCP and Skills become live managed views.** Real daemon
  data replaces fixtures and Mock badges: list with add/edit/delete,
  origin-free single-source model (files under `config/` are the only
  source), per-file load-error display, MCP test-connection panel
  showing discovered tools or typed failures, skill detail with a
  markdown body editor, and daemon-unreachable states. Draft semantics
  for these sections end — they manage real config now.
- **Daemon guard hardening.** The loopback guard gains trusted-Host
  validation on every request (reads included — today's guard checks
  only mutations): MCP lists expose authored env values and the probe
  spawns configured commands, so a DNS-rebound Host must not reach
  them. Legitimate loopback clients (CLI, desktop shell, dev proxy)
  are unaffected.
- **Env values are config-file content.** MCP `env` maps round-trip
  through the loopback-enforced, Host-validated API as plain config
  (the user authored them in a file); the keyring remains the only
  daemon-managed secret store and no new secret handling is
  introduced.

**Out of scope:** runtime wiring (MCP tools into agent tool calls, skill
injection into agent context) — that is an engine change on the existing
tools seam; MCP prompts/resources browsing (the probe lists tools only);
skill bundle upload/install from archives or marketplaces; CRUD for the
other registry kinds (prompts, agents, hooks); enable/disable states.

## Capabilities

### New Capabilities

- `mcp-management`: Daemon-managed MCP server lifecycle — CRUD over
  `config/mcps/*.yaml` with immediate registry apply, name-as-file-stem
  rules, transport validation, and the connection-probe behavior (tool
  discovery and typed failures).

- `skill-management`: Daemon-managed skill lifecycle — CRUD over
  `config/skills/<dir>/SKILL.md` directories with frontmatter
  validation, slug-directory rules, bundle-inclusive deletion and
  rename semantics.

### Modified Capabilities

- `api-contract`: Adds the MCP and skill endpoints with typed
  201/200/204/400/404/409 responses and error codes, the skill body
  field, MCP test results (tools or typed failure reasons), and
  per-kind list bounds consistent with the contract's conventions.

- `config-registry`: The registry gains serialized write paths for the
  MCP and skill kinds (atomic writes, synchronous publish, watcher
  coexistence) and exposes load errors alongside snapshots; the
  read-only loader contract otherwise stands.

- `settings-shell`: The MCP and Skills sections stop being fixture
  previews and become live daemon views with management actions, load
  states, and typed failure handling, mirroring what Providers became.

## Impact

- **Backend:** `internal/registry` (dual-transport manifest loading,
  write paths + error exposure),
  new `internal/mcpclient` (go-sdk probe behind a small interface; fake
  in tests), `internal/server` (MCP + skills handlers, guard hardening,
  apigen regeneration), `cmd/zbotd` wiring, `go.mod` +
  `github.com/modelcontextprotocol/go-sdk`.
- **Contract & codegen:** additive endpoints/schemas only; TS client and
  Go types regenerate; no breaking changes.
- **Frontend:** Settings MCP and Skills sections reworked to live data,
  `shared/api/client.ts` methods, route-mocked Playwright flows
  including failure paths.
- **Docs:** `backend/internal/registry/AGENTS.md` (writes), new
  `internal/mcpclient/AGENTS.md`, `internal/server/AGENTS.md`,
  product-context, frontend README.
- **Verification:** unit tests use fake probes and `httptest` (zero
  live MCP servers or network in `go test`); manual verification runs a
  real stdio server (e.g. a tiny go-sdk echo server) and records the
  probe transcript.
