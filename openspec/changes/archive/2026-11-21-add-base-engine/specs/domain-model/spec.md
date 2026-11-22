# Spec Delta

## MODIFIED Requirements

### Requirement: AgentEngine seam

`domain.AgentEngine` SHALL remain the single engine seam (Name, RunGoal, Resume, Cancel) with engine implementations selected by configuration and isolated in `internal/engine/<name>/`; only the active engine's package imports its framework. `GoalRequest` SHALL carry an optional qualified model reference (`provider/model-id`) that implementations use for model resolution; implementations that cannot resolve a model SHALL fail the run with guidance rather than inventing a default.

#### Scenario: Engine integrates via the seam

- **WHEN** an engine implementation is selected by configuration
- **THEN** every consumer depends only on the domain seam and the implementation's framework imports stay inside its package

#### Scenario: Goal run streams events

- **WHEN** a goal run executes
- **THEN** every agent step, tool call, and lifecycle transition reaches consumers as a TimelineEvent through the seam

#### Scenario: No model resolvable

- **WHEN** RunGoal is called with no model and no agent-level model
- **THEN** the run fails fast with guidance to select a model
