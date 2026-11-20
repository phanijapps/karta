# Spec Delta

## ADDED Requirements

### Requirement: Provider list endpoint

The contract SHALL define a provider list endpoint returning the configured providers, each with name, type, base URL, credential environment-variable name when set, and a flag for whether that credential is currently configured. No response field SHALL carry a secret value.

#### Scenario: List providers without secrets

- **WHEN** the provider list endpoint is called on a running daemon
- **THEN** it returns 200 with the configured providers and no field carries a credential value

### Requirement: Provider models endpoint

The contract SHALL define a provider models endpoint that proxies OpenAI-compatible model discovery for a named provider. It SHALL return 200 with the discovered model identifiers, 404 with a typed error body for an unknown provider, or 502 with a typed error body distinguishing provider unreachable, provider authentication failure, and malformed provider response.

#### Scenario: Discover models through the daemon

- **WHEN** the provider models endpoint is called for a configured, reachable provider
- **THEN** it returns 200 with the provider's discovered model identifiers

#### Scenario: Unknown provider

- **WHEN** the provider models endpoint is called with a name that is not configured
- **THEN** it returns 404 with a typed error body

#### Scenario: Provider failure is typed

- **WHEN** the provider is unreachable, rejects credentials, or returns a malformed model list
- **THEN** the endpoint returns 502 with a typed error body identifying which failure occurred
