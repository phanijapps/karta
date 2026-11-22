# Spec Delta

## MODIFIED Requirements

### Requirement: Evidence and confirmation semantics

The host SHALL stamp every accepted observation with a contribution
identity (source, session, execution, message sequence, subject, kind,
normalized content hash). Replaying an identical contribution SHALL be
idempotent: no confidence change, no new contribution; if the record it
created has since been superseded, the replay disposition is
`ignored_superseded`. The same content under a different subject or
kind is DISTINCT evidence. A distinct contribution matching an active
record SHALL advance `mention_count` and `last_confirmed_at` and
converge confidence as `c' = c + (1 − c) · α` with `α` configurable
(default 0.3). Contributions SHALL be retained and auditable through
the domain seam.

#### Scenario: Replay of identical evidence is idempotent
- **WHEN** the same observation (same contribution identity) is delivered twice
- **THEN** the second delivery changes no confidence, mention, or contribution entry

#### Scenario: Replay after revision is ignored
- **WHEN** identical evidence is replayed after its record was superseded
- **THEN** the disposition is `ignored_superseded` and nothing is refreshed

#### Scenario: Distinct evidence converges confidence
- **WHEN** the same subject/kind/content is observed from two different sessions
- **THEN** confidence rises toward but not to 1.0 per the formula, and both contributions are retained
