# Proposal

## Why

Karta cannot reach any model today: `~/.zbot/config.yaml` has no provider
schema, no daemon HTTP surface exists to expose configuration, and the
Settings → Providers section is fixture-only with "not yet supported" copy.
Product truth demands "no cloud LLM lock-in — providers are configured, not
hardcoded", and the architecture brief already reserves `config.yaml` for
"Engine selection, LLM providers, …". Starting with the OpenAI-compatible
family (local ollama today, z.ai and any OpenAI-compatible endpoint) gives
the smallest provider plane that is real end-to-end and locally testable.

## What Changes

- **Provider configuration.** A `providers:` list in `~/.zbot/config.yaml`
  (strict typed loading): `name`, `type: openai-compatible`, `base_url`
  (the OpenAI-compatible API root, e.g. `http://localhost:11434/v1`), and
  optional `api_key_env` naming an environment variable. Credentials are
  referenced by env-var name only — secret values never persist under
  `~/.zbot/`.
- **New `backend/internal/providers` package.** Resolves providers from
  config and performs OpenAI-compatible model discovery (`GET
  {base_url}/models`) with a bearer token when a credential is configured,
  a bounded timeout, and typed errors (unreachable / auth failure /
  malformed response). Unit tests use in-process `httptest` fakes; zero
  live network in `go test`.
- **First minimal daemon surface.** New `backend/internal/server` (chi,
  loopback bind from `server.address`) and `cmd/zbotd`, implementing
  `GET /healthz`, `GET /api/providers`, and
  `GET /api/providers/{name}/models`. Goal/session endpoints remain
  contract-only until the engine change lands. This change owns the Go
  server-type generator decision anticipated by the scaffold change (D5):
  `oapi-codegen` output joins `task codegen`.
- **Contract (additive).** `contracts/openapi.yaml` gains the two provider
  endpoints plus `Provider`, `Model`, and provider-error schemas; TS client
  and Go types regenerate from it.
- **Frontend goes live for Providers.** Settings → Providers loads real
  providers from the daemon, shows credential-env status, and runs a Test
  connection that lists discovered models or a typed error state. It stays
  read-only: `config.yaml` remains user-owned and hand-edited (source hint
  points at it). MCP/Skills/Logging/Advanced sections keep their fixture
  previews. A Vite dev proxy bridges the SPA to the loopback daemon for
  verification.
- **Docs.** New package `AGENTS.md` files, product-context status update,
  and an ollama example configuration.

**Out of scope (recorded deliberately):** engine integration (providers
used in runs — the eino adapter consumes this plane later), chat
completions/streaming, provider CRUD or config mutation via the API,
non-OpenAI-compatible provider types, and hot reload (provider changes
apply on daemon restart).

## Capabilities

### New Capabilities

- `model-providers`: Configured OpenAI-compatible model providers —
  resolution from `config.yaml`, env-referenced credentials, model
  discovery with typed failures, and the rule that no secret value is
  persisted or exposed.

### Modified Capabilities

- `config-plane`: Adds the `providers` config key with strict validation
  (unique names, non-empty `name`/`base_url`, http(s) URL, supported type)
  that hard-fails startup naming the offending entry; an absent key is a
  valid empty list.
- `api-contract`: Adds provider endpoints — listing configured providers
  without secret values, and proxying provider model discovery with typed
  404/502 error responses (additive within the version).
- `settings-shell`: The Providers section changes from a fixture preview to
  a live, read-only daemon-backed view with connection testing; the draft
  lifecycle requirement is scoped to the sections that remain previews.

## Impact

- **Backend:** `internal/config` (providers schema + validation),
  new `internal/providers`, new `internal/server`, new `cmd/zbotd`;
  `go.mod` gains `chi` (runtime) and `oapi-codegen` (tooling). Downward
  dependency graph is respected: providers sits beside config, server sits
  above both.
- **Contract & codegen:** `contracts/openapi.yaml` (additive),
  `Taskfile.yml` codegen task grows the Go server-type generation step;
  `frontend/src/shared/api/schema.d.ts` regenerates.
- **Frontend:** `features/settings` Providers section, a small
  `shared/api` client module for the provider endpoints, Vite dev proxy,
  Playwright tests route-mock the daemon API.
- **Docs:** `backend/internal/providers/AGENTS.md`,
  `backend/internal/server/AGENTS.md`, `docs/product/product-context.md`,
  frontend README (dev proxy / running against zbotd).
- **Sequencing:** builds on the in-flight `add-desktop-ui-shell` change
  (its `settings-shell` spec is the base being modified); apply and archive
  that change first.
- **Manual verification:** against the locally installed ollama
  (`http://localhost:11434/v1`) — configure it in `~/.zbot/config.yaml`,
  run `zbotd`, see the provider in Settings, and list its models.
