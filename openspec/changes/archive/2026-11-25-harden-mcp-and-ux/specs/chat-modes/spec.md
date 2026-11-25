# Spec Delta

## MODIFIED Requirements

### Requirement: Session CRUD

*(unchanged; ADDED scenario for titling)*

#### Scenario: A research shell titles itself from its first analysis

- **WHEN** an untitled research session's first analysis produces a
  primary intent
- **THEN** the session takes a label derived from it (sanitized, ≤ 64
  chars); a user-set title is never overwritten
