# Spec Delta

## ADDED Requirements

### Requirement: Intent analysis card is live

Settings → Advanced SHALL include a live Intent Analysis card backed by
the daemon: a provider/model picker fed by the model catalog (with each
model's context window when known), a read-only derivation line (model,
window and its source, output allowance, packet estimate, trimming), and
a **Test analysis** action. Saving SHALL persist through the daemon API
and apply without restart; the card SHALL expose no token knobs; an
unreachable daemon SHALL show the connection state instead of stale
values.

#### Scenario: Model selection applies without restart
- **WHEN** the user picks a provider/model on the Intent Analysis card and saves
- **THEN** the daemon's intent-analysis settings update and the next root research run analyzes with that model

#### Scenario: The derivation is read-only and model-derived
- **WHEN** the card shows a model whose window was discovered, tabled, or unknown
- **THEN** the derivation line names the window, its source, the full-window output allowance, the packet estimate, and "no trimming" — and offers no field to change any of them

#### Scenario: Test analysis reports the decision and its cost
- **WHEN** the user runs Test analysis
- **THEN** the card reports status, model, tokens in/out, duration, attempts, the contract summary, and whether the safety valve fired, or the failure reason

#### Scenario: Unreachable daemon shows state, not fixtures
- **WHEN** the daemon is unreachable
- **THEN** the card shows the connection state and no fabricated values
