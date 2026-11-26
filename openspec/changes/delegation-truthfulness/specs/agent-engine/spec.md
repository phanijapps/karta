# Spec Delta

## ADDED Requirements

### Requirement: Assistant rows persist per-step usage

The engine SHALL write each completion's prompt and completion token usage on the assistant row it produces, for root and delegated runs alike, so a run's cost is attributable per step — not only per execution.

#### Scenario: A run's cost is readable per step
- **WHEN** a root run completes three model steps
- **THEN** each assistant row carries that step's prompt and completion tokens, and their sum equals the execution's totals
