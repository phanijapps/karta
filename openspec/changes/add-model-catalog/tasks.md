# Tasks

## 1. Contract & codegen (contract-first)

- [x] 1.1 Add `GET /api/models` (flat `{provider, id}` models + per-provider `refreshed_at` timestamps, 200 always, never dials providers) and `POST /api/providers/{name}/models/refresh` (200 with fetched models, 404 unknown, 502 typed failure) to `contracts/openapi.yaml`; verify `npx @redocly/cli lint contracts/openapi.yaml` passes
- [x] 1.2 Regenerate Go server types and the TS client via `task codegen`; verify a second run produces zero diff and `task check:frontend` passes with the new types

## 2. Catalog (backend)

- [x] 2.1 Implement `Catalog` in `internal/providers` (mutex-guarded per-provider entries with refresh timestamps; `Set`, `Get`, `Snapshot` with stable ordering; in-memory only); verify unit tests cover set/snapshot ordering, empty-start, and concurrent access (`-race`)
- [x] 2.2 Update the existing discovery handler to warm the catalog on every successful model fetch (Test connection and refresh share one path); verify a handler test proves a successful discovery is visible in `GET /api/models` immediately

## 3. Daemon surface

- [x] 3.1 Implement `GET /api/models` (instant snapshot read, never dials) and `POST /api/providers/{name}/models/refresh` (atomic credential resolution → live fetch → catalog update on success only; typed 404/502, catalog untouched on failure) in `internal/server`; verify handler tests (httptest + fake provider) cover every api-contract delta scenario including non-destructive failure and refresh-warming
- [x] 3.2 Update `backend/internal/providers/AGENTS.md` and `backend/internal/server/AGENTS.md` for the catalog and refresh semantics; verify `cd backend && go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass

## 4. Frontend

- [x] 4.1 Add `getModels()` and `refreshProviderModels(name)` to `shared/api/client.ts` (typed by the generated schema; discriminated refresh result); verify `pnpm check` passes from `frontend/` and no fetches exist outside the client module
- [x] 4.2 Replace the header fixture model selector with the catalog-backed picker: grouped-by-provider options from `GET /api/models`, in-memory selection labeled as simulated, disabled-with-guidance empty state pointing at Settings → Providers, no fixture fallback; retire the fixture `models` array from the header path; verify the mock-chat delta scenarios are demonstrable in the running UI
- [x] 4.3 Add a per-provider "Refresh models" action in Settings → Providers (framed so Test connection and refresh both clearly update the catalog); verify the settings copy is honest in the running UI
- [x] 4.4 Update Playwright tests with route mocks: catalog read, picker grouping + selection, empty-catalog state, refresh success and typed failure; verify `pnpm test` passes from `frontend/` without a live daemon

## 5. End-to-end verification & docs

- [x] 5.1 Manually verify against the local ollama: refresh from the UI, see the picker list real models grouped under the provider, `curl /api/models` shows entries with timestamps, daemon idle issues no provider requests, and a failed refresh (stopped endpoint) keeps the previous list; record the command log in the change evidence note
- [x] 5.2 Update `docs/product/product-context.md`, `docs/architecture/frontend-shell.md`, and `frontend/README.md` for the live picker and manual-refresh catalog; verify docs claim only what task 5.1 demonstrated
- [x] 5.3 Confirm constitution invariants and run `task verify` green end-to-end

## 6. Review and hardening

- [x] 6.1 Run the read-only `zbot-reviewer` subagent over the change scope, change artifacts, and the five constitution boundaries; fix every P0/P1 finding and fix P2s or record in the change summary why not; verify findings are resolved with targeted tests
- [x] 6.2 Re-run `task verify` after the final review fixes and documentation edits; verify it is green with no generated drift or unrelated files before declaring implementation complete

## Workflow follow-up

- Archive only after the review task is complete: reviewer ran, all P0/P1 findings fixed, and `task verify` green after the final edit.
