# Design

## Context

Model discovery exists (`Client.ListModels`, discovery endpoint, Test
connection in Settings) but is fire-and-forget: nothing remembers the
result. The header model selector is a fixture (`entities/session`
`models` array, "Mock" badge). User decisions: both consumers (chat
picker + cross-provider API), **manual refresh only** — no background
polling, no TTL, no startup fetch.

## Goals / Non-Goals

**Goals:**

- One in-memory catalog per daemon: per-provider model lists + refresh
  timestamps; instant reads; explicit refreshes.
- `GET /api/models` for any client; a live grouped picker in the header.

**Non-Goals:**

- Background/periodic refresh, TTL expiry, persistence across restarts
  (catalog starts empty each start — recorded trade-off of manual mode).
- Model metadata beyond ids, default-model pinning, engine wiring.

## Decisions

### D1: Catalog lives beside discovery in `internal/providers`

A small `Catalog` (catalog.go): mutex-guarded
`map[provider]entry{models []Model, refreshedAt time.Time}` with
`Set(provider, models)`, `Snapshot()` (copy-on-read, stable order by
provider name then list order), and `Get(provider)`. In-memory only —
local-first stays byte-clean (no writes under `~/.zbot/`), and a restart
resetting to empty is exactly the manual-freshness contract. The server
owns one instance; the existing discovery handler calls `Set` on success,
so Test connection and the refresh endpoint share one warming path.

### D2: Refresh is one explicit endpoint, failures non-destructive

`POST /api/providers/{name}/models/refresh` → `ResolvedLookup` (atomic
credential resolution, unchanged) → live `ListModels` → on success
`Catalog.Set` + 200 with the list; on typed failure → 502 with reason,
catalog untouched. Reusing `ResolvedLookup` keeps the CSRF-era security
invariants (origin guard applies — POST requires JSON content type; the
guard already covers it since it's method-based, path-agnostic). GET
`/api/models` maps the snapshot to `{models: [{provider, id}],
refreshed_at: {name: ts}}` (a map schema with date-time values;
additionalProperties typed, bounded count via the existing 64-provider
cap).

### D3: Flat catalog shape; grouping is a client concern

Entries carry `provider` + raw `id` (no `provider/id` composite): clients
group and namespace as they like (picker groups; CLI can flatten to
`ollama/glm-4.7`). Refresh timestamps travel per provider so consumers
can display freshness without policy — staleness is visible, never
enforced.

### D4: Picker replaces the fixture selector honestly

The header select becomes `<optgroup>`-per-provider from `GET /api/models`
(via `client.ts`), selection in `$state` in the session entity (retiring
the fixture `models` array from the header path). While the engine is
absent, the selected model is labeled "simulated replies" — the picker
changes what's selected, not what replies. Empty catalog → disabled
select with "Refresh models in Settings → Providers" (the refresh paths
live there: Test connection or per-provider Refresh). No fixture
fallback.

### D5: Frontend plumbing stays thin

`client.ts` gains `getModels()` (typed by the generated schema) and
`refreshProviderModels(name)` (discriminated result like the other
mutations). Settings → Providers gains a per-provider "Refresh models"
button (same endpoint; replaces/augments Test connection copy: "Test
connection (updates the model catalog)") — one small settings-shell copy
change, no spec delta there. Playwright route-mocks cover: catalog read,
picker grouping, empty state, refresh success/failure.

### D6: No eventbus emission (unchanged reasoning)

Catalog reads/refreshes are not agent steps, tool calls, hook decisions,
or compaction events; no session/timeline context exists. Recorded again
so review treats the omission as deliberate.

## Risks / Trade-offs

- [Empty catalog until first refresh] → explicit in the picker's empty
  state; startup fetch deliberately rejected (manual mode).
- [Stale lists mislead] → timestamps in the API + honest UI; a future
  change can add TTL/background refresh without contract changes.
- [Flat list grows with models] → bounded by providers (≤64) × realistic
  model counts; the contract caps the array generously.

## Migration Plan

Additive contract; no data migration; catalog is ephemeral. Rollback =
revert.
