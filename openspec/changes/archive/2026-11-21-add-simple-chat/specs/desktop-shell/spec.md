# Spec Delta

## MODIFIED Requirements

### Requirement: Daemon-independent local preview

The shell SHALL start and function without a daemon or credentials. Runtime state SHALL stay in memory; mock surfaces SHALL NOT use browser persistence, telemetry, external assets, provider requests, daemon HTTP/SSE, or filesystem writes. Live daemon views (providers, model catalog, chat) SHALL degrade to visible connection states without fixture fallback, and fixture content SHALL be clearly identified as mock data.

#### Scenario: Offline preview

- **WHEN** the built shell is served locally with no daemon and external network access blocked
- **THEN** navigation, themes, settings, and the chat's daemon-connection state remain usable using only local static assets
- **AND** reloading resets UI state and no localStorage, sessionStorage, IndexedDB, or application cookies have been written
