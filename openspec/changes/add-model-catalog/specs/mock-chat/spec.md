# Spec Delta

## MODIFIED Requirements

### Requirement: Model and action previews are honest

The chat header SHALL offer a model selector backed by the daemon's live model catalog, grouped by provider, with selection held in memory only and labeled as simulated while replies are mock. An empty catalog SHALL show an explicit empty state pointing at Settings → Providers — never fixture models. Unimplemented actions SHALL be omitted or visibly unavailable with an explanation; message content SHALL render as safe text.

#### Scenario: Select a mock model

- **WHEN** the user opens the selector after a provider refresh and picks a model
- **THEN** models are listed grouped by provider and the header reflects the choice in memory, labeled as simulated until the engine lands

#### Scenario: Empty catalog is honest

- **WHEN** no provider has been refreshed
- **THEN** the selector shows an empty state directing to Settings → Providers and offers no fixture models

#### Scenario: Untrusted message text

- **WHEN** a user submits text containing HTML or script-like markup
- **THEN** it appears as inert text rather than executable content
