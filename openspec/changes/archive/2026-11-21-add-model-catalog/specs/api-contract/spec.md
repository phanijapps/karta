# Spec Delta

## ADDED Requirements

### Requirement: Model catalog endpoint

The contract SHALL define `GET /api/models` returning the daemon's catalog: a flat `models` array of `{ provider, id }` entries plus per-provider `refreshed_at` timestamps, served from memory (200, possibly empty). The endpoint SHALL NOT trigger provider requests.

#### Scenario: Read the catalog

- **WHEN** `GET /api/models` is called
- **THEN** it returns 200 with the catalogued models and refresh timestamps, without contacting any provider

### Requirement: Provider refresh endpoint

The contract SHALL define `POST /api/providers/{name}/models/refresh` performing a live model fetch for one provider, updating the catalog, and returning the fetched list. It SHALL return 404 for an unknown provider and 502 with the typed provider-failure reason (unreachable, unauthorized, malformed, credential unavailable) without changing the catalog.

#### Scenario: Refresh a provider

- **WHEN** the refresh endpoint is called for a reachable provider
- **THEN** it returns 200 with the fetched models and the catalog is updated

#### Scenario: Refresh an unknown provider

- **WHEN** the refresh endpoint is called with an unconfigured name
- **THEN** it returns 404 with a typed error body

#### Scenario: Refresh failure is typed and non-destructive

- **WHEN** the provider fails during refresh
- **THEN** it returns 502 with the failure reason and the catalog keeps the previous entry
