# Spec Delta

## ADDED Requirements

### Requirement: Distillation settings endpoint

The contract SHALL define `GET /api/distillation` returning the
current distillation settings (provider name, model id, prompt text)
and `PUT /api/distillation` accepting a partial update of the same
shape, applied by the daemon without restart (daemon-managed settings
file; provider and model validated against the configured providers).
GET SHALL reflect daemon truth; PUT of an unknown provider or empty
model SHALL fail with a typed error.

#### Scenario: Round-trip settings
- **WHEN** `PUT /api/distillation` sets provider and model, then `GET` is called
- **THEN** the response carries the saved values and the next distillation pass uses them

#### Scenario: Unknown provider rejected
- **WHEN** `PUT /api/distillation` names a provider that is not configured
- **THEN** the daemon responds with a typed error naming the provider
