# Spec Delta

## Purpose

Ward hosting: sticky per-session wards, the Active Ward packet, and the per-ward orchestrator.


## ADDED Requirements

### Requirement: The ward orchestrates its own sessions

A ward SHALL be able to declare its orchestrator model in
`.zbot/config.yaml` (`provider`/`model`; either empty inherits the
daemon default). When a session bound to that ward runs, the ward's
model SHALL execute the hosted session — the intent analysis AND the
root run — in place of the picker default. The ward detail SHALL
expose the override for the UI.

#### Scenario: A bound ward overrides the picker default

- **WHEN** a session bound to a ward declaring `provider`/`model`
  runs a research send with a different picker default
- **THEN** the analysis and the root run execute on the ward's model
  and the transcript rows record that provider/model

#### Scenario: An unconfigured ward inherits

- **WHEN** the ward declares no override (or only one of the pair)
- **THEN** the picker default runs the session unchanged
