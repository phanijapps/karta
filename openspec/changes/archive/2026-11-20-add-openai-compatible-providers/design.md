# Design

## Context

Today `backend/internal/` has only `domain`, `config`, `registry`, and
`eventbus` — no HTTP server, no `cmd/zbotd`, no engine adapter, and no
provider concept. `config.Config` strictly decodes `engine`, `server`,
`compaction`, `wards`. The contract (`contracts/openapi.yaml`) defines
health/goal/session endpoints ahead of any implementation, and the scaffold
change's D5 explicitly deferred the Go server-type generator to "the server
change". The frontend Settings → Providers section is fixture-driven
(in-flight `add-desktop-ui-shell` change), already sketched around
OpenAI-compatible fields. A local ollama serves
`http://localhost:11434/v1/models` for manual verification. Constraints:
channel-agnostic core, contract-first API, local-first storage, downward
dependency graph in `backend/AGENTS.md`.

## Goals / Non-Goals

**Goals:**

- A provider plane the future eino adapter can consume unchanged:
  configure → resolve → discover models.
- The first real daemon surface (`internal/server` + `cmd/zbotd`), minimal
  but honest: only what the provider endpoints need.
- Frontend Providers section backed by real daemon data with typed failure
  states.

**Non-Goals:**

- Chat completions, streaming, or any engine-run behavior (belongs to the
  engine change; the providers package is shaped to extend, not rewritten).
- Provider CRUD: the daemon never writes `config.yaml` (config-plane rule:
  bootstrap creates structure, never content).
- Hot reload of provider config; registry `fsnotify` stays scoped to the
  config tree, and provider changes apply on restart.
- Non-OpenAI-compatible provider types (the `type` field reserves the
  extension point).

## Decisions

### D1: Providers live in `config.yaml`, not `config/providers/`

The architecture brief names `config.yaml` as the home for "LLM providers";
providers are daemon wiring (URLs, env references), not user content
artifacts like prompts/agents/skills. Strict typed loading and startup
hard-fail semantics already exist there. Alternative — a registry kind with
hot reload — was rejected for this slice: provider changes are rare, and
keeping the registry content-only preserves its shape.

### D2: `base_url` is the OpenAI-compatible API root, used verbatim

`base_url` points at the root that serves `/models` (later
`/chat/completions`), version prefix included: ollama
`http://localhost:11434/v1`, z.ai per its OpenAI-compatible docs. The
client appends only `/models` — no trailing-slash normalization, no
auto-`/v1`. Verbatim use keeps the mapping to provider documentation
obvious; the config-plane delta documents this with a scenario.

### D3: Credentials by env-var reference, resolved at request time

`api_key_env` names a variable read from the daemon's environment only
in memory for the outbound request. Nothing secret is persisted under
`~/.zbot/` or returned by the API (responses carry the variable *name*
plus a `credential_configured` boolean). Unset/empty variable → no
Authorization header (ollama needs none; a provider that requires auth
will answer 401, which surfaces as the typed auth failure). Alternative —
a keyring or `~/.zbot/credentials` file — deferred; it adds storage
surface without changing this slice's shapes.

### D4: New `internal/providers` package — resolution + discovery client

Placement respects the dependency graph (imports `domain` and `config`,
sits beside `config`). Surfaces: `Lookup(name)` resolving the configured
list (unknown name → typed `ErrUnknownProvider`), and a discovery client
`ListModels(ctx, provider)` returning `[]Model` or a typed error
(`ErrUnreachable`, `ErrUnauthorized`, `ErrMalformed`) with a ~10s timeout,
no retries. Errors are sentinel-wrapped so handlers map them to contract
statuses without string matching. Discovery parses the OpenAI list-models
shape (`{"object":"list","data":[{"id":...}]}`), tolerating extra fields.
Chat completion is deliberately absent — the engine change adds it with
streaming decisions in one place.

### D5: `internal/server` + `cmd/zbotd` — minimal chi, oapi-codegen types

This change *is* the anticipated "server change", so it owns the generator
decision: `oapi-codegen` (std `net/http` interface mode, matching the
project's chi/net-http stack) generating server types into
`backend/internal/server/apigen/`, committed and drift-checked by
`task codegen` (TS client generation unchanged). The server mounts only
`GET /healthz`, `GET /api/providers`, `GET /api/providers/{name}/models`;
contracted-but-unimplemented operations (goals/sessions) ride the
generated `apigen.Unimplemented` stubs and answer 501 — clients can
tell "planned by the contract" from "unknown route", and every contracted
path stays mounted so nothing drifts as operations land. `cmd/zbotd` is a thin main: resolve home, bootstrap, load config,
serve on `server.address` (loopback). Handler→client mapping: unknown
provider → 404; `ErrUnreachable`/`ErrUnauthorized`/`ErrMalformed` → 502
with a typed `reason` field. The daemon dials providers from its own
process (no CORS, no secret in the browser) — the frontend never talks to
providers directly.

### D6: No eventbus emission for provider listing/discovery

Constitution rule 4 enumerates agent steps, delegations, tool calls, hook
decisions, and compaction — provider discovery is none of these and has no
session or timeline context. Recorded here so review treats the omission
as deliberate; provider *use* inside engine runs will emit when the engine
lands.

### D7: Frontend — live read-only Providers, route-mocked tests

A small `shared/api` client module (typed by the generated schema) fetches
providers and models. Settings → Providers becomes a read-only list with
per-provider Test connection showing discovered model ids or the typed
failure with recoverable guidance (e.g. "is ollama running?"); the
`~/.zbot/config.yaml` source hint explains that adding providers means
editing the file. Daemon-unreachable shows an explicit connection state —
never fixtures. Other sections keep their fixture previews and draft
lifecycle. Vite dev server proxies `/api` and `/healthz` to
`127.0.0.1:8787` for manual verification; Playwright tests stub the daemon
with `page.route` (no live daemon in the test run). Alternative — serving
`frontend/dist` from zbotd — deferred to the desktop shell change, which
owns embedding.

### D8: Sequencing against `add-desktop-ui-shell`

The `settings-shell` delta modifies requirements that change currently
defines (it is implemented but not archived). Apply and archive
`add-desktop-ui-shell` first; `openspec validate` already flags the
ordering, and archive merges its deltas into the main specs this change
then modifies.

## Risks / Trade-offs

- [Provider `/v1/models` responses vary across OpenAI-compatible servers]
  → parse only the documented `data[].id` shape, ignore unknown fields;
  manual verification against the installed ollama catches shape drift.
- [Restart required for provider changes] → accepted and documented;
  config-plane loading is strict-by-design, and hot reload is a separate
  concern.
- [Env-var credentials visible to daemon child processes] → accepted for a
  local-first single-user system; no secret ever crosses the API boundary.
- [First server surface grows beyond the slice] → non-goal enforcement via
  tasks; goals/sessions stay 501 stubs, no SSE, no session store.
- [oapi-codegen adds a tooling dependency] → committed generated output
  keeps `go test` hermetic; `task codegen` owns regeneration and drift
  checks (same discipline as the TS client).

## Migration Plan

Additive only: the contract gains endpoints, `config.yaml` gains an
optional key (absent key = empty list, so existing homes keep working).
Rollback is reverting the change; no data or config migration exists.
