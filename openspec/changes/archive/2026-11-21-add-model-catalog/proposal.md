# Proposal

## Why

Model lists exist only inside the Settings "Test connection" button —
fetched once, shown, forgotten. The next changes (chat model selection,
the engine) need a current view of what models each provider offers, and
other clients (CLI, tools) have no way to ask "what models do I have?".
The catalog should be a first-class daemon resource with an explicit,
user-controlled refresh.

## What Changes

- **In-memory model catalog in the daemon.** `internal/providers` grows a
  `Catalog`: per-provider model lists plus a refresh timestamp, held in
  memory only (nothing persisted under `~/.zbot/`). It starts empty and is
  populated by explicit refreshes.
- **Manual refresh only.** No background polling, no startup fetch, no
  TTL. A refresh happens when the user asks: a new
  `POST /api/providers/{name}/models/refresh` endpoint fetches live and
  updates the catalog, and every successful Test connection (existing
  discovery) updates it too — one code path, no surprise traffic to
  providers.
- **Cross-provider models API.** `GET /api/models` returns every
  catalogued model (`provider` + `id`) with per-provider refresh
  timestamps — instant, served from the catalog, never blocking on a
  provider. Any client can consume it.
- **Live model picker in the chat header.** The fixture "Mock model"
  selector is replaced by a real picker fed from `GET /api/models`,
  grouped by provider, with an explicit empty state pointing at Settings →
  Providers to refresh. Selection stays in-memory and honest: replies
  remain simulated until the engine change lands.
- **Docs** updated for the catalog behavior and refresh semantics.

**Out of scope (recorded):** background/periodic refresh, TTL staleness
policy, persistence across daemon restarts, model metadata beyond ids
(owned_by, context windows), default-model selection, and engine
consumption (a later change wires runs to the selected provider/model).

## Capabilities

### New Capabilities

- `model-catalog`: The daemon's in-memory catalog of per-provider model
  lists — manual refresh semantics, refresh-on-discovery coherence,
  instant reads, staleness transparency, and the cross-provider listing
  contract.

### Modified Capabilities

- `api-contract`: Adds `GET /api/models` (catalog read) and
  `POST /api/providers/{name}/models/refresh` (live fetch + catalog
  update, typed failures), additive.
- `mock-chat`: The header model selector becomes catalog-backed live
  data (grouped by provider, honest about empty/stale state and simulated
  replies) instead of fixture models.

## Impact

- **Backend:** `internal/providers` (new `catalog.go`: snapshot + mutex;
  discovery success feeds it), `internal/server` (two handlers), apigen
  regeneration. No storage, no timers.
- **Contract & codegen:** additive endpoints/schemas; TS client
  regenerates.
- **Frontend:** chat header selector rework (`entities/session` fixture
  models retired from the header), `client.ts` catalog methods, Settings
  Providers unchanged (Test connection now also warms the catalog —
  surfaced in its copy), route-mocked Playwright coverage.
- **Docs:** package AGENTS.md, product-context, frontend README.
- **Verification:** unit tests with httptest fakes; manual E2E against
  local ollama (refresh → picker lists models; `curl /api/models`).
