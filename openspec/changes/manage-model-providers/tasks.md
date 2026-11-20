# Tasks

## 1. Contract & codegen (contract-first)

- [x] 1.1 Add provider management endpoints and schemas to `contracts/openapi.yaml` — `POST /api/providers`, `PATCH /api/providers/{name}`, `DELETE /api/providers/{name}` with typed 201/200/204/400/404/409/503 responses, error codes (`duplicate_name`, `origin_protected`, `keyring_unavailable`, `provider_limit`), the optional `credential` request object (`none`/`env`+name/`keyring`+one-shot key), `Provider.origin` (`file`|`config`) and `Provider.credential_status` (`unset`|`ready`|`missing`|`unavailable`), and `credential_unavailable` added to the discovery failure reason enum; verify `npx @redocly/cli lint contracts/openapi.yaml` passes
- [x] 1.2 Preflight `github.com/zalando/go-keyring`: pin a version, record platform backends (D-Bus/Keychain/wincred), cgo/subprocess behavior, blocking-call and error-mapping characteristics in the design notes; verify the module builds against `backend/go.mod` and the findings are recorded
- [x] 1.3 Regenerate Go server types and the TS client via `task codegen`; verify a second run produces zero diff and `task check:frontend` passes with the new types

## 2. Config plane and providers store

- [x] 2.1 Extend `internal/config`: add `providers` to `Subdirs` (bootstrap/self-heal create `config/providers/`), reject non-loopback `server.address` hosts at load (`127.0.0.0/8`, `::1`, `localhost` pass), and keep config.yaml provider entries environment-reference-only (unknown `api_key` key rejected there); verify table-driven tests cover the new directory, loopback accept/reject matrix, and the marker-rejected-in-config case
- [x] 2.2 Build the provider `Store` in `internal/providers`: load config.yaml entries first then `config/providers/*.yaml` (duplicates within/across sources, combined count above the list bound, malformed files, and stem≠name are load errors naming sources; last-good set survives breakage), managed-name validation (`^[a-z0-9][a-z0-9-]{0,63}$` minus Windows-reserved names), atomic temp-file+rename writes, and `Create/Update/Delete` with origin checks, rename file-swaps, and `Origin` on every provider; verify unit tests cover merge order, name/stem rules including case and reserved names, CRUD file effects, origin protection, capacity, and concurrent-safe writes
- [x] 2.3 Add fsnotify watching of `config/providers/` to the store, serialized with mutations by the store's reload mutex so a reload can never publish a stale set over a completed mutation; verify deterministic interleaving tests (not only `-race`) for external edit/delete during a mutation, competing creates, and externally-corrupted files keeping the last-good set with the error logged
- [x] 2.4 Update `backend/internal/providers/AGENTS.md` and `backend/internal/config/AGENTS.md` for the store, sources, origin, name, and loopback rules; verify `golangci-lint run` passes in `backend/`

## 3. Secret store (keyring)

- [x] 3.1 Define the `SecretStore` seam — `Set/Get/Delete(ctx, name)` with a typed `ErrKeyringUnavailable` and a ~3s per-call deadline wrapper — and the zalando/go-keyring implementation (service `Karta`, user `provider:<name>@<homeHash>`, the hash of the resolved home); verify unit tests run entirely against a fake store and two-home isolation (same provider name, different homes, different keys) is covered
- [x] 3.2 Wire credentials through with the fixed mutation order and compensation: create/edit stores the key before the file commit and compensates a file failure by deleting the just-written secret (orphan accepted, logged, overwritten on reuse); rename writes new-key→new-file→remove-old-file→delete-old-key leaving the old provider operational on failure; delete removes the file then the secret best-effort; discovery and status resolve env or keyring at request time (missing → no credential + `credential_status: missing`; unavailable → typed `credential_unavailable` failure, never a silent unauthenticated request); verify fake-store tests cover set/get/delete, re-key on rename, missing-item idempotence, injected failures at every boundary (secret-write fail, file-commit fail, compensation fail, old-key delete fail), slow-store deadline behavior, and delete-during-discovery
- [x] 3.3 Verify keyring unavailability is typed end-to-end (fake store returning `ErrKeyringUnavailable` → 503 on create/edit, `credential_status: unavailable` in listings, `credential_unavailable` 502 on discovery) and that no fallback storage path exists anywhere (grep + test)

## 4. Daemon CRUD surface

- [x] 4.1 Implement create/edit/delete handlers in `internal/server` using generated types: validate→secret→file→publish flow under the store lock, credential-object semantics (absent retains, `none` clears, `env`/`keyring` replaces), 409 duplicate/capacity/origin, 503 keyring, 404 unknown, and immediate visibility in list/models after mutation; verify handler tests (httptest + fake provider + fake secret store) cover every api-contract delta scenario including PATCH semantics, origin protection, capacity, and compensation paths
- [x] 4.2 Update `cmd/zbotd` wiring to the store + secret store (loopback-validated address) and confirm startup with both sources at once; verify a manual smoke run on a fresh test home (create via curl → list → test connection against local ollama → edit → rename → delete → file tree states) and record output in the change notes
- [x] 4.3 Update `backend/internal/server/AGENTS.md` and the `cmd/zbotd` note for CRUD, origin, and mutation-order semantics; verify `cd backend && go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass

## 5. Frontend management UI

- [x] 5.1 Extend `shared/api/client.ts` with typed create/update/delete methods (typed duplicate/origin/keyring/capacity failures); verify `pnpm check` passes from `frontend/` and no hand-rolled types or fetches exist outside the client module
- [x] 5.2 Rework Settings → Providers: add-provider form (name with managed-name validation, base URL, optional credential choice), edit and delete (confirmation naming the stored credential) for file-origin providers, origin badges with a config.yaml hint for config-origin entries, credential-status display (ready/missing/unavailable), and refresh-after-mutation with drafts preserved on failure; verify the settings-shell delta scenarios are demonstrable in the running UI against the dev daemon
- [x] 5.3 Update Playwright tests with route-mocked CRUD flows: add success, duplicate 409 recovery, capacity 409, keyring 503 recovery, invalid-name 400, origin-protected edit disabled/409, delete confirmation, edit-retains-credential, edit refresh, and daemon-unreachable states; verify `pnpm test` passes from `frontend/` without a live daemon or keyring
- [x] 5.4 Update `frontend/README.md` (provider management, keyring behavior and the headless-Linux caveat); verify the documented commands run as written from `frontend/`

## 6. Constitution, end-to-end verification & docs

- [x] 6.1 Amend the root `AGENTS.md` in this change: add the scoped exception that provider credential values may live in the platform keyring (nothing else moves outside `~/.zbot/`; plaintext fallback stays prohibited) and add `config/providers/` to the documented control-plane tree; verify the amended constitution matches the design's keyring scoping exactly
- [x] 6.2 Manually verify the full slice against the running daemon and local ollama on a THROWAWAY home (`ZBOT_HOME` temp, so the home-scoped keyring cannot touch the real home): add a duplicate-URL ollama provider, test connection, edit base URL, rename, add a keyring-credential provider (or record the typed keyring-unavailable error if no Secret Service is available), delete providers, and confirm `~/.zbot/config.yaml` was never modified; record the command log and screenshots in the change evidence note
- [x] 6.3 Update `docs/product/product-context.md`, `docs/architecture/frontend-shell.md`, and the root README: provider management is live, keyring storage and its caveats, config.yaml entries read-only with restart semantics unchanged; verify docs claim only what task 6.2 demonstrated
- [x] 6.4 Confirm constitution invariants (no internal package imports clients/cmd, eino boundary clean, `task codegen` zero drift) and run `task verify` green end-to-end

## 7. Review and hardening

- [x] 7.1 Run the read-only `zbot-reviewer` subagent over the change scope, change artifacts, and the five constitution boundaries; fix every P0/P1 finding and fix P2s or record in the change summary why not; verify findings are resolved with targeted tests
- [x] 7.2 Re-run `task verify` after the final review fixes and documentation edits; verify it is green with no generated drift or unrelated files before declaring implementation complete

## Workflow follow-up

- Archive only after the review task is complete: reviewer ran, all P0/P1 findings fixed, and `task verify` green after the final edit.
