# Design

## Context

The provider plane (archived `add-openai-compatible-providers`) resolves
providers strictly from `config.yaml` at daemon startup; Settings →
Providers is a read-only view; the `model-providers` spec forbids secret
values under `~/.zbot/`. The registry hot-reloads
`config/{prompts,agents,skills,hooks,mcps}` via fsnotify with atomic
snapshots and a `reloadMu` serializing load→swap. User decisions: full
CRUD, OS keyring for secrets, immediate apply. A pre-implementation
review (13 findings, all folded in) sharpened atomicity, naming,
keyring-scoping, and loopback enforcement.

## Goals / Non-Goals

**Goals:**

- Add/edit/delete providers from Settings with zero YAML editing and no
  daemon restart; a key entered once lands in the platform keyring.
- `config.yaml` stays exactly as authored — the daemon never writes it.
- Hand-editing remains a first-class path (`config/providers/*.yaml`).

**Non-Goals:**

- New provider types (still `openai-compatible` only).
- Migrating existing `config.yaml` entries into files (they keep working,
  read-only via API).
- Default-provider/model pinning (engine change), registry CRUD for other
  kinds, import/export, watching `config.yaml` itself.

## Decisions

### D1: CRUD persists per-provider files, not config.yaml rewrites

Daemon-managed providers live at `~/.zbot/config/providers/<name>.yaml`;
`config.yaml` entries remain read-only hand-authored (environment
credential references only there — the keyring marker is file-only, so a
hand-authored entry can never reference a secret nobody provisioned).
Loading merges config.yaml entries first, then files; duplicates within
or across sources and a combined count above the contract's 64-entry list
bound are load errors. This supersedes the archived change's D1
rationale: management turns a managed provider into content. Files are
the 1:1 CRUD unit and the user's global config stays untouched.

### D2: Store + watcher in `internal/providers`, with real last-good semantics

A `Store` owns load, CRUD, and watching: atomic snapshot swaps, a mutex
serializing mutations AND watcher reloads (the registry's `reloadMu`
pattern — an atomic pointer alone cannot stop a stale reload from
publishing over a completed mutation), and last-good-set behavior: a
reload that fails (malformed file, duplicate, overflow) keeps the last
good snapshot and logs the error (the registry swaps despite errors —
deliberately NOT copied). External changes land within the watcher
settle; API mutations publish synchronously before responding.
`Origin` (`config`|`file`) rides every provider. In-flight discovery
requests keep the provider they resolved at request start (delete or
rename mid-request does not cancel them).

### D3: SecretStore seam over zalando/go-keyring; home-scoped addresses

`SecretStore` interface — `Set/Get/Delete(ctx, name)` returning a typed
`ErrKeyringUnavailable` — implemented over
`github.com/zalando/go-keyring` (a preflight task pins the version and
records its platform behavior: D-Bus on Linux, cgo-free, blocking calls,
error mapping). Addresses: service `Karta`, user
`provider:<name>@<homeHash>` where `<homeHash>` is 8 hex chars of the
resolved home path — relocated homes (test roots, `ZBOT_HOME`) can never
share or clobber each other's secrets. Every call carries a context with
a ~3s deadline enforced by the wrapper, so a stalled Secret Service
cannot stall listings (statuses resolve concurrently per provider) or
discovery. Fakes in unit tests; the real keyring is manual verification.

Preflight record (pinned v0.2.8): package-level `Set/Get/Delete(service,
user)` plus `ErrNotFound`/`ErrUnsupportedPlatform`; backends are Linux
Secret Service via `godbus/dbus/v5` (pure Go, no cgo), macOS Keychain via
the `security` CLI subprocess, and Windows Credential Manager via
`danieljoos/wincred` syscalls. Calls are blocking and take no context —
the wrapper's deadline goroutine bounds them. `keyring.MockInit()`
exists for tests; this change still uses its own fake to inject
unavailable/stalled stores and keep unit tests free of keyring-lib
behavior.

### D4: Credential semantics — explicit object, one-shot value, compensation

Create/edit bodies carry an optional `credential` object: `{kind: none}`
(clears), `{kind: env, name}` (reference), `{kind: keyring, key}` (the
one-shot value; the only place a secret crosses the loopback API).
Absent object on edit = retain. This kills the PATCH omitted/null/empty
ambiguity that generated pointer types cannot express. Files store
`api_key_env: NAME` or `api_key: keyring` (marker string; any other
value rejected). Mutation order is fixed — validate → prepare secret
(keyring write first) → commit file (temp+rename) → publish — with
compensation: file-commit failure after a keyring write deletes the
just-written secret (a failed compensation leaves an orphan at a stable
address that the daemon cannot read and that name-reuse overwrites;
logged). Delete removes the file first, then the secret best-effort — a
failed cleanup does not block the 204 (documented residual, overwritten
on reuse). Rename writes the new key under the new address, commits the
new file, removes the old file, then deletes the old secret; any failure
leaves the old provider operational.

### D5: Contract grows additive CRUD; origin + credential_status gate the UI

`POST /api/providers` (201/400/409-duplicate/409-capacity/503-keyring),
`PATCH /api/providers/{name}` (200/400/404/409/503),
`DELETE /api/providers/{name}` (204/404/409-origin). `Provider` gains
required `origin` (`file`|`config`) and `credential_status`
(`unset`|`ready`|`missing`|`unavailable`) — new required *response*
fields alongside the closed schema; our regenerated client is the only
consumer, and the evolution rule is reconciled in D9. `ProviderFailureReason`
gains `credential_unavailable` (keyring stall during discovery → typed
502, never a silent unauthenticated request upstream). Error codes:
`duplicate_name`, `origin_protected`, `keyring_unavailable`,
`provider_limit`.

### D6: Loopback is enforced, not declared

The unauthenticated secret-write and destructive CRUD surface must not
depend on an OpenAPI `servers` entry. `config.Load` now hard-fails a
`server.address` whose host is not loopback (`127.0.0.0/8`, `::1`,
`localhost`) — a config-plane requirement in this change. Existing
loopback defaults are unaffected.

### D7: Managed names are portable file stems

Daemon-created names match `^[a-z0-9][a-z0-9-]{0,63}$` and reject
Windows-reserved device names (case-insensitive) — no case-insensitive
collisions, no colons, no overlong stems; a file whose stem differs from
its declared name is a load error. Config.yaml names keep the wider Id
pattern (never filenames). The add form's client-side validation mirrors
the rule before the first request.

### D8: No eventbus emission (unchanged reasoning)

Provider CRUD is still not an agent step, tool call, hook decision, or
compaction event; the provider list has no session/timeline context.
Recorded again so review treats the omission as deliberate.

### D9: Frontend — inline management, route-mocked tests

Settings → Providers adds the add form (name, base URL, optional
credential choice), per-provider edit and delete-with-confirmation for
file-origin providers, origin badges ("Managed" vs "config.yaml"), and
refresh-after-mutation; drafts survive failure states with typed
guidance (duplicate, capacity, keyring). `client.ts` gains typed
create/update/delete (still the only fetch module). Playwright
route-mocks cover CRUD happy paths plus 409/409/503/400 failures and
daemon-down. Contract-evolution note: required response-field additions
are accepted within our single-client version line and recorded here
rather than claimed as universally additive.

## Risks / Trade-offs

- [Keyring unavailable on headless Linux] → typed 503/credential_status
  guidance; env-referenced and no-credential providers work regardless.
- [macOS/Windows keyring paths untested here] → `SecretStore` seam keeps
  platform code thin; preflight task records backend behavior; CI stays
  Linux; manual verification records the gap.
- [zalando/go-keyring maintenance drift] → pinned version + preflight
  before codegen lands; the seam makes replacement local.
- [Compensation leaves orphan secrets] → accepted residual at a stable
  address, unreadable without a referencing file, overwritten on reuse;
  logged. Full 2-phase commit rejected as over-engineering for a
  single-user local daemon.
- [Two declaration sources confuse users] → origin badges + docs.
- [Required response fields vs closed schemas] → single client,
  regenerated together; recorded in D9.

## Migration Plan

Additive contract; bootstrap gains one empty directory. Root `AGENTS.md`
gains the scoped keyring exception and the providers dir in the same
change (constitution rule: rule changes update the constitution file).
No data migration. Rollback = revert; provider files are inert without
the daemon code.
