# model-providers Specification

## Purpose

Configured model providers: how Karta resolves OpenAI-compatible providers from the control-plane configuration, performs model discovery against them, surfaces typed failures, and keeps credential values out of storage, responses, and logs.

## Requirements

### Requirement: Providers resolve from configuration by unique name

The system SHALL resolve the provider list from `config.yaml` at daemon startup. Each provider SHALL carry a unique non-empty `name`, a supported `type`, and an OpenAI-compatible API-root `base_url`. Providers SHALL be addressable by that name, and a name that is not configured SHALL be reported as a distinct unknown-provider error rather than a connection failure.

#### Scenario: Resolve a configured provider

- **WHEN** `config.yaml` declares a provider and the system looks it up by name
- **THEN** the provider's type and base URL resolve for use in discovery

#### Scenario: Unknown provider name

- **WHEN** a caller references a provider name that is not configured
- **THEN** the system reports an unknown-provider error distinct from any network or authentication failure

### Requirement: Credential values are referenced, never stored or exposed

A provider MAY reference a credential by environment-variable name (`api_key_env`). The variable's value SHALL be read from the daemon environment only in memory and only for an outbound request, and SHALL NOT be written under `~/.zbot/`, returned by any API response, or included in error messages or logs. When the variable is unset or empty, requests SHALL be sent without credentials and the provider's listing SHALL report the credential as not configured.

#### Scenario: Credential value never leaves the process boundary

- **WHEN** a provider with a configured credential performs model discovery
- **THEN** the credential is used only in the outbound request and appears in no stored file, API response, or log line

#### Scenario: Referenced credential is missing

- **WHEN** a provider references an environment variable that is unset and model discovery is requested
- **THEN** the request is sent without credentials and the provider listing shows the credential as not configured

### Requirement: OpenAI-compatible model discovery with typed failures

Model discovery SHALL call the provider's model-list endpoint at `{base_url}/models` with an Authorization bearer header when a credential is configured, bounded by a timeout. Outcomes SHALL be typed: a successful model list (parsed from the OpenAI-compatible list response), provider unreachable, provider authentication failure, and malformed provider response. Each failure SHALL be distinguishable by callers without inspecting raw error text.

#### Scenario: Discover models from a local runtime

- **WHEN** discovery runs against a configured OpenAI-compatible provider (for example a local ollama at `http://localhost:11434/v1`)
- **THEN** the model identifiers from its model-list response are returned

#### Scenario: Provider rejects credentials

- **WHEN** the provider answers discovery with an authentication failure
- **THEN** the system reports a typed authentication failure

#### Scenario: Provider is unreachable

- **WHEN** the provider endpoint cannot be reached or does not answer within the bound
- **THEN** the system reports a typed unreachable failure

#### Scenario: Provider response is not a valid model list

- **WHEN** the provider answers with a body that is not a parseable OpenAI-compatible model list
- **THEN** the system reports a typed malformed-response failure

### Requirement: Provider behavior is verifiable without live services

Automated tests of provider resolution and discovery SHALL run against in-process fake HTTP servers and SHALL make no live network calls. Verification against a real provider (for example a locally installed ollama) SHALL be a manual step, not part of the automated test suite.

#### Scenario: Automated suite stays offline

- **WHEN** the automated tests run
- **THEN** all provider scenarios exercise in-process fakes and no request leaves the machine
