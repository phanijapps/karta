# Proposal

## Why

Providers today require hand-editing `~/.zbot/config.yaml` and restarting
zbotd — the Settings → Providers view is read-only by design. Adding z.ai
or a second ollama endpoint means leaving the UI, editing YAML, and
restarting the daemon. The provider plane is real; managing it should be
too.

## What Changes

- **Full provider CRUD over the contract.** `POST /api/providers`
  (create), `PATCH /api/providers/{name}` (edit, including rename), and
  `DELETE /api/providers/{name}` — alongside the existing list and
  model-discovery endpoints. Mutations apply immediately: the daemon's
  provider set reloads without a restart, and the response reflects the
  change.
- **Daemon-managed provider files.** CRUD operates on per-provider files
  under a new `~/.zbot/config/providers/` directory (`<name>.yaml`).
  `config.yaml`'s `providers:` list remains supported, hand-authored, and
  read-only via the API (its entries surface with a `config` origin and
  the UI points at the file); both sources merge, duplicate names across
  sources are a load error. The daemon never rewrites `config.yaml`.
- **Credentials in the OS keyring.** Creating or editing a provider via
  the API can set an API key; the daemon stores it once in the platform
  keyring (Linux Secret Service, macOS Keychain, Windows Credential
  Manager) and never persists it under `~/.zbot/`, in `config.yaml`, in
  API responses, or in logs. Provider entries reference the keyring by
  `api_key: keyring`; the existing `api_key_env` (environment reference)
  stays fully supported. A missing or unavailable keyring is a typed,
  user-actionable error — never a silent plaintext fallback. Deleting a
  provider deletes its keyring item; renaming re-keys it.
- **Settings → Providers becomes manageable.** Add-provider form (name,
  base URL, optional API key), edit, delete-with-confirmation, live
  refresh after every mutation, origin badges, and typed failure states
  (duplicate name, validation, keyring unavailable). Config-origin
  providers remain read-only with an "edit in config.yaml" hint.
- **Bootstrap grows the providers directory.** First-run bootstrap and
  self-heal create `config/providers/`; existing homes are untouched
  except for the new empty directory.
- **Docs & constitution.** Root `AGENTS.md` gains a scoped exception for
  platform-keyring credential storage and the new providers directory;
  package guides and product/architecture docs updated (config ownership
  rules, keyring behavior, restart no longer required for managed
  provider changes).

**Out of scope:** non-OpenAI-compatible provider types, model pinning or
default-provider selection (engine change), CRUD for prompts/agents and
other registry kinds, and any write path into `config.yaml` itself.

## Capabilities

### New Capabilities

- `provider-management`: Daemon-managed provider lifecycle — the
  `config/providers/` directory (load, merge with config.yaml entries,
  write, delete, hot apply), the OS-keyring secret store behind
  `api_key: keyring`, and the CRUD behavior rules (immediate apply,
  origin, rename re-key, keyring-failure semantics).

### Modified Capabilities

- `model-providers`: Credential sources broaden from "environment
  variable reference" to env-var OR keyring; discovery sends the keyring
  value at request time; the "never stored or exposed" guarantee is
  restated for both sources (still nothing under `~/.zbot/`).
- `api-contract`: Adds the create/edit/delete provider endpoints with
  typed 201/200/204/400/404/409 responses and a 503 keyring-unavailable
  error; `Provider` gains `origin` and a keyring credential reference.
- `config-plane`: Bootstrap and self-heal create `config/providers/`;
  the provider-list requirement now names both declaration sources with
  cross-source duplicate names rejected.
- `settings-shell`: The Providers section gains add/edit/delete with
  live refresh and typed failures; config-origin entries stay read-only
  with a source hint.

## Impact

- **Backend:** `internal/config` (Subdirs + validation unchanged for
  config.yaml entries), `internal/providers` (dir load/write/delete,
  fsnotify hot apply, keyring seam), new `internal/providers/keyring.go`
  behind a small `SecretStore` interface (zalando/go-keyring
  implementation; fake in tests), `internal/server` (CRUD handlers,
  apigen regeneration), `go.mod` + keyring dep.
- **Contract & codegen:** additive endpoints/schemas; TS client and Go
  types regenerate; existing endpoints unchanged (no breaking changes —
  `origin` is a new required field on responses, additive within the
  version).
- **Frontend:** Settings Providers section CRUD flows, client module
  methods, route-mocked Playwright tests including failure paths.
- **Docs:** package AGENTS.md files, product-context, frontend README,
  config ownership notes.
- **Verification:** unit tests use a fake `SecretStore` and httptest
  fakes (zero live keyring/network in `go test`); manual verification
  covers CRUD against the running daemon and — where a Secret Service is
  present — a real keyring write/read; headless-Linux keyring
  unavailability is demonstrated as a typed error, not a failure.
