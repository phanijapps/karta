# Tasks

## 1. Contract & codegen (contract-first)

- [x] 1.1 Add the provider endpoints and schemas to `contracts/openapi.yaml` — `GET /api/providers` (list, no secret values, `credential_configured` flag) and `GET /api/providers/{name}/models` (200 model list, 404 unknown provider, 502 typed provider failure with `reason`), each with `operationId`, `additionalProperties: false`, bounded identifiers; verify `npx @redocly/cli lint contracts/openapi.yaml` passes
- [x] 1.2 Extend the `codegen` task in `Taskfile.yml` to generate Go server types with `oapi-codegen` (std-net-http interface mode) into `backend/internal/server/apigen/`, committed alongside the TS client; verify `task codegen` regenerates both and a second run produces zero diff
- [x] 1.3 Regenerate `frontend/src/shared/api/schema.d.ts` from the updated contract; verify `task check:frontend` still passes with the new types in place

## 2. Config plane: provider list

- [x] 2.1 Add the `providers` list to `internal/config` (typed `ProviderConfig`: `name`, `type`, `base_url`, `api_key_env`) with strict decoding, absent-key-means-empty, and validation (unique non-empty names, `type` = `openai-compatible`, absolute http(s) `base_url` used verbatim); verify table-driven tests cover every config-plane delta scenario and `go test ./internal/config/` passes
- [x] 2.2 Document the provider key in the config package AGENTS.md (fields, base-URL-is-API-root convention, env-reference credentials) and verify the existing config-plane spec scenarios still hold (`go test ./...` in `backend/`)

## 3. Providers package

- [x] 3.1 Create `backend/internal/providers`: resolve the configured list with `Lookup(name)` returning a typed unknown-provider error; verify unit tests cover configured and unknown-name resolution
- [x] 3.2 Implement the OpenAI-compatible discovery client `ListModels` (`GET {base_url}/models`, bearer header only when the referenced env var is set and non-empty, ~10s bound, no retries) returning model ids or typed errors (unreachable / unauthorized / malformed); verify httptest-based unit tests cover success, auth failure, unreachable, malformed body, timeout, and the missing-credential-sent-without-header case with zero live network calls
- [x] 3.3 Add `backend/internal/providers/AGENTS.md` (responsibility, boundaries, entrypoints) and verify `golangci-lint run` in `backend/` passes

## 4. Daemon surface: server + zbotd

- [x] 4.1 Create `backend/internal/server`: chi router on the configured loopback `server.address` mounting `GET /healthz`, `GET /api/providers`, `GET /api/providers/{name}/models` using the generated types; map unknown provider → 404 and typed provider errors → 502 with `reason`; verify handler tests (httptest + fake provider server) cover every api-contract delta scenario
- [x] 4.2 Create `cmd/zbotd`: resolve home (flag then `ZBOT_HOME`), bootstrap, strict-load config, serve; verify a manual smoke run (`go run ./cmd/zbotd`) answers `/healthz` and `/api/providers` with an empty list on a fresh `ZBOT_HOME` test root
- [x] 4.3 Add `backend/internal/server/AGENTS.md` and a `cmd/zbotd` package note (wiring only, no logic); verify `go vet`, `golangci-lint run`, and `go test -race ./...` pass in `backend/`

## 5. Frontend: live Providers section

- [x] 5.1 Add a `shared/api` client module for the provider endpoints typed by the generated schema, and proxy `/api` + `/healthz` to `127.0.0.1:8787` in the Vite dev config; verify `pnpm check` passes and the client has no hand-rolled schema types
- [x] 5.2 Rework the Settings Providers section to a live read-only view: load configured providers (name, type, base URL, credential env name + configured status), per-provider Test connection listing discovered models or the typed failure with recoverable guidance, `~/.zbot/config.yaml` source hint, and an explicit daemon-unreachable state with no fixture fallback; remove provider fixtures from `settingsFixtures()` while other sections keep theirs; verify the settings-shell delta scenarios are demonstrable in the running UI
- [x] 5.3 Update Playwright settings tests to route-mock the daemon provider endpoints (list, models success, each typed 502 failure, 404, and daemon-unreachable) instead of asserting fixture providers; verify `pnpm test` passes without a live daemon
- [x] 5.4 Update `frontend/README.md` with the dev-proxy assumption and how to run against `zbotd`; verify the documented commands run as written

## 6. End-to-end verification & docs

- [x] 6.1 Manually verify the slice against the local ollama: write a `providers` entry with `base_url: http://localhost:11434/v1` (no credential) into a test home's `config.yaml`, run `zbotd`, open Settings → Providers, run Test connection, and confirm real model ids render; record the outcome (command log + screenshot) in the change notes or evidence doc
- [x] 6.2 Update `docs/product/product-context.md` status: Providers section is daemon-backed and read-only, other Settings sections remain previews, engine integration still pending; verify the doc claims nothing beyond what task 6.1 demonstrated
- [x] 6.3 Confirm constitution invariants: no `backend/internal/` package imports frontend/desktop/cmd code, boundary check clean, and `task codegen` shows zero drift after all edits; verify `task verify` runs end-to-end green

## 7. Review and hardening

- [x] 7.1 Run the read-only `zbot-reviewer` subagent over the change scope, change artifacts, and the five constitution boundaries; fix every P0/P1 finding and fix P2s or record in the change summary why not; verify findings are resolved with targeted tests
- [x] 7.2 Re-run `task verify` after the final review fixes and documentation edits; verify it is green with no generated drift or unrelated files before declaring implementation complete

## Workflow follow-up

- Apply and archive `add-desktop-ui-shell` first — its `settings-shell` spec is the base this change's delta modifies.
- Archive this change only after the review task is complete: reviewer ran, all P0/P1 findings fixed, and `task verify` green after the final edit.
