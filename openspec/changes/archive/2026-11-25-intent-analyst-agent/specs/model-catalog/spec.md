# Spec Delta

## MODIFIED Requirements

### Requirement: Catalog holds per-provider model lists in memory

The daemon SHALL maintain a catalog of model lists keyed by provider, each
with the timestamp of its last successful refresh. Each entry SHALL carry
the model's context window when it is known — discovered from the
provider at refresh (Ollama `/api/show`, the `<family>.context_length`
value) — and absent otherwise; an absent window never fails the refresh.
The catalog (entries with their windows) SHALL persist under `~/.zbot/`
and restore on restart without dialing any provider. Catalog reads SHALL
be instant snapshot reads that never dial a provider.

#### Scenario: Fresh daemon has an empty catalog
- **WHEN** the daemon starts with no persisted catalog
- **THEN** the catalog is empty and `GET /api/models` returns no models

#### Scenario: Reads never block on providers
- **WHEN** `GET /api/models` is called while a provider is unreachable
- **THEN** it answers immediately from the catalog

#### Scenario: An Ollama refresh discovers windows
- **WHEN** a provider that answers Ollama's native API is refreshed
- **THEN** every listed model's entry carries its `context_length` as the context window

#### Scenario: A non-Ollama refresh carries no window
- **WHEN** a provider without Ollama's native API is refreshed
- **THEN** its entries carry no context window and the refresh succeeds

#### Scenario: Windows survive a restart
- **WHEN** the daemon restarts after a refresh that discovered windows
- **THEN** the restored entries carry the same windows with no provider traffic

### Requirement: Refresh is manual and explicit

The catalog SHALL be populated only by explicit refreshes: a client-invoked refresh endpoint, or a successful model discovery (the Settings Test connection). The daemon SHALL NOT poll providers in the background, fetch at startup, or expire entries on a timer. A failed refresh SHALL keep the previous list and report the typed failure. Window discovery SHALL run only inside a refresh, bounded in time, and a discovery failure for one model SHALL leave that entry's window absent rather than failing the refresh.

#### Scenario: Explicit refresh updates the catalog
- **WHEN** the refresh endpoint is called for a reachable provider
- **THEN** the catalog's entry for that provider is replaced with the fetched list and its refresh timestamp updated

#### Scenario: Failed refresh keeps the last list
- **WHEN** a refresh fails (unreachable, unauthorized, malformed, credential unavailable)
- **THEN** the catalog keeps the provider's previous list and timestamp, and the typed failure is returned

#### Scenario: No background traffic
- **WHEN** the daemon runs idle with configured providers
- **THEN** it issues no model requests on its own

#### Scenario: Discovery warms the catalog
- **WHEN** model discovery succeeds for a provider
- **THEN** the catalog reflects that provider's fetched list as if refreshed

### Requirement: Cross-provider catalog listing

`GET /api/models` SHALL return every catalogued model as a flat list of provider name plus model id, together with each provider's last refresh timestamp and each entry's context window when known, so clients can group, namespace, and display freshness without extra calls.

#### Scenario: List models across providers
- **WHEN** two providers have been refreshed
- **THEN** the response carries both providers' models with their refresh timestamps

#### Scenario: Staleness is visible, not enforced
- **WHEN** a provider was refreshed long ago or never
- **THEN** its models (or absence) and timestamp are reported as-is; no entry is hidden or expired by policy

#### Scenario: Windows ride the listing
- **WHEN** an entry has a discovered or persisted context window
- **THEN** the listing carries it as `context_window`; entries without one omit the field
