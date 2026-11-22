# Spec Delta

## MODIFIED Requirements

### Requirement: Advanced configuration previews

Advanced SHALL display clearly labeled engine, server address,
compaction-budget, and wards-root previews associated with the
existing configuration concepts. It SHALL distinguish existing
concepts from proposed UI-only settings and disable actions requiring
daemon mutation or filesystem access — **except the Distillation
card, which is a live control** (below).

#### Scenario: Preview advanced configuration
- **WHEN** the Advanced section is opened
- **THEN** the read-only previews render as before, visually distinct from the live Distillation card

### Requirement: Distillation card is live

Settings → Advanced SHALL include a live Distillation card backed by
the daemon: a provider/model picker fed by the model catalog and a
prompt editor (the daemon's current distillation prompt). Saving SHALL
persist through the daemon API and apply without restart; failures
SHALL surface inline without discarding unrelated drafts; when the
daemon is unreachable the card SHALL show the connection state instead
of stale fixtures.

#### Scenario: Model selection persists live
- **WHEN** the user picks a provider/model in the Distillation card and saves
- **THEN** the daemon's distillation settings update and the next distillation pass uses them

#### Scenario: Prompt round-trips
- **WHEN** the user edits and saves the prompt
- **THEN** reopening Settings shows the saved prompt verbatim (daemon-sourced, never fixtures)

#### Scenario: Unreachable daemon shows state, not fixtures
- **WHEN** the daemon is unreachable
- **THEN** the Distillation card shows the connection state and no fabricated values
