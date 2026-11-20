# Spec Delta

## Purpose

The daemon's in-memory model catalog: per-provider model lists refreshed manually, warmed by discovery, read instantly by any client.

## ADDED Requirements

### Requirement: Catalog holds per-provider model lists in memory

The daemon SHALL maintain a catalog of model lists keyed by provider, each with the timestamp of its last successful refresh, held in memory only — nothing persisted under `~/.zbot/` and nothing carried across daemon restarts. Catalog reads SHALL be instant snapshot reads that never dial a provider.

#### Scenario: Fresh daemon has an empty catalog

- **WHEN** the daemon starts
- **THEN** the catalog is empty and `GET /api/models` returns no models

#### Scenario: Reads never block on providers

- **WHEN** `GET /api/models` is called while a provider is unreachable
- **THEN** it answers immediately from the catalog

### Requirement: Refresh is manual and explicit

The catalog SHALL be populated only by explicit refreshes: a client-invoked refresh endpoint, or a successful model discovery (the Settings Test connection). The daemon SHALL NOT poll providers in the background, fetch at startup, or expire entries on a timer. A failed refresh SHALL keep the previous list and report the typed failure.

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

`GET /api/models` SHALL return every catalogued model as a flat list of provider name plus model id, together with each provider's last refresh timestamp, so clients can group, namespace, and display freshness without extra calls.

#### Scenario: List models across providers

- **WHEN** two providers have been refreshed
- **THEN** the response carries both providers' models with their refresh timestamps

#### Scenario: Staleness is visible, not enforced

- **WHEN** a provider was refreshed long ago or never
- **THEN** its models (or absence) and timestamp are reported as-is; no entry is hidden or expired by policy
