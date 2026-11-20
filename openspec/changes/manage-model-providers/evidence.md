# Provider management verification

## Task 4.2 — CRUD smoke (throwaway home, real ollama)

Home `/tmp/zbot-crud-smoke` with a config.yaml `ollama` entry (origin
`config`); daemon built from `cmd/zbotd` after the CRUD implementation.

```
POST   /api/providers {"name":"ollama-copy","base_url":"http://localhost:11434/v1"}
       → 201 {"origin":"file","credential":"none","credential_status":"unset",...}
GET    /api/providers
       → ollama (origin config) + ollama-copy (origin file)
GET    /api/providers/ollama-copy/models
       → {"models":[{"id":"glm-5.3:cloud"},{"id":"deepseek-v4.1-flash:cloud"},…]}   # real ollama, no restart
PATCH  /api/providers/ollama-copy {"name":"ollama-two"} → 200
DELETE /api/providers/ollama            → 409 origin_protected (config.yaml entry)
DELETE /api/providers/ollama-two        → 204; config/providers/ empty afterwards
grep ollama config.yaml                 → still present (file never modified)
```

Backend suite at this point: `go vet`, `golangci-lint` 0 issues,
`go test -race ./...` green across config/providers/server; largest file
434 lines (store.go) — no god classes.

## Task 6.2 — Full E2E (throwaway home, real ollama, real keyring)

Home `/tmp/zbot-e2e-home` (config.yaml `ollama` entry); daemon + preview
server; driven by Playwright against the real UI. The machine has a
Secret Service (gnome-keyring via D-Bus), so the keyring path is real.

**Phase A** (e2e-created-and-tested.png): Added `ollama-copy` (no
credential) via the form — appeared immediately, Test connection listed
real ollama models. Added `zai-key` with a one-shot API key — badge
"Managed · Credential set". Verified out-of-band:

- keyring probe: Get("Karta", "provider:zai-key@a7068089") returned the
  exact one-shot value (home-scoped address, hash of the throwaway home);
- config.yaml byte-identical to the pre-run snapshot (diff clean);
- config/providers/{ollama-copy,zai-key}.yaml on disk;
- daemon list shows origin config/file and credential_status unset/ready.

**Phase B** (e2e-renamed-rekeyed.png, e2e-delete-confirm.png): Edited
zai-key → zai-prod (credential retained, secret re-keyed — badge stayed
"Credential set"). Delete confirmation named the provider and its stored
credential; after confirm the provider was gone, only ollama-copy.yaml
remained, and the keyring probe returned "secret not found in keyring"
for provider:zai-prod@a7068089.

Result: PASS — full CRUD + keyring lifecycle against real components;
config.yaml never modified; the throwaway home's keyring namespace never
touched the real home.

## Review (task 7.1) — gpt-6.1-sol

Verdict: **BLOCK — 4 P0, 5 P1, 3 P2**. All twelve findings fixed (none
deferred):

- **P0-1 CSRF exfiltration** (hostile page creates an env-credential
  provider aimed at the attacker; discovery sends the secret): mutations
  now require a JSON content type (kills the form-post vector) and an
  Origin that is absent (CLI) or loopback/same-host; regression tests
  reproduce the exact exfiltration attempt (`guard.go`, `guard_test.go`).
- **P0-2 lost key on failed same-name edit**: the prior secret is backed
  up and restored when the file commit fails (store.go Update).
- **P0-3 timed-out keyring writes racing later ones**: KeyringStore fences
  underlying completions behind an op mutex — a late write can never land
  after a newer one; fence test included.
- **P0-4 create/rename clobbering unseen external files**: no-clobber
  install (temp + link) returning a typed conflict; both paths tested.
- **P1-5** unchanged-credential edits touch no secret at all; keyless
  keyring on edit retains the stored key (contract description updated,
  codegen regenerated).
- **P1-6** discovery resolves provider+credential atomically under the
  store mutex (`ResolvedLookup`), so delete/rename cannot strip an
  in-flight request's credential.
- **P1-7** listing resolves credential statuses concurrently (order
  preserved).
- **P1-8** env-name validation shared across config, files, and mutation
  inputs; env+keyring markers mutually exclusive in files.
- **P1-9/P2-12** frontend: blank-key rules (create requires; edit retains
  only when already keyring) and reserved-device-name mirroring with the
  rule named.
- **P2-10** PATCH typing notes the generated ProviderUpdate shape.
- **P2-11** added the missing regression coverage: barrier-style
  atomicity/fence/restore tests backend-side; browser tests for reserved
  names, blank key, keyring-unavailable with a key entered, and capacity.

Post-fix verification (task 7.2): `task verify` green end-to-end — codegen
byte-stable, golangci-lint 0 issues, `go test -race ./...` all packages,
svelte-check 0/0, clean build, 23/23 Playwright tests. Largest hand-written
files: store.go 499 lines, Providers.svelte 320 — no god classes (generated
artifacts excepted).
