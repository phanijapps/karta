# Spec Delta

## MODIFIED Requirements

### Requirement: Credential values are referenced, never stored or exposed

A provider MAY reference a credential by environment-variable name (`api_key_env`) or, for file-declared providers, the keyring marker (`api_key: keyring`). The value SHALL be read only in memory, for request or status use, and SHALL NOT be written under `~/.zbot/`, returned in any response, or logged. An unset source sends no credential and reports the credential missing; an unavailable keyring fails with a typed credential-unavailable outcome, never a silent unauthenticated request.

#### Scenario: Credential value never leaves the process boundary

- **WHEN** a provider with a configured credential performs model discovery
- **THEN** the credential is used only in the outbound request and appears in no stored file, API response, or log line

#### Scenario: Referenced credential is missing

- **WHEN** a provider references an environment variable that is unset, or a keyring item that is absent, and model discovery is requested
- **THEN** the request is sent without credentials and the provider listing shows the credential as missing

#### Scenario: Keyring unavailable during discovery

- **WHEN** discovery runs for a keyring-credential provider while the keyring cannot be reached within its deadline
- **THEN** the daemon reports a typed credential-unavailable failure instead of sending an unauthenticated request upstream
