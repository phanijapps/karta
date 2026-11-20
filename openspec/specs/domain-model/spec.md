# domain-model Specification

## Purpose

The pure domain layer defining z-Bot's core types and the interface seams that decouple the daemon from any agent engine, framework, or client.

## Requirements

### Requirement: Domain purity

The domain package SHALL contain types and interfaces only, import no other internal package, and depend on nothing outside the Go standard library.

#### Scenario: Purity check

- **WHEN** the domain package's imports are inspected
- **THEN** no `zbot/backend/internal/...` package and no third-party module appears, only stdlib

#### Scenario: Purity enforced by test

- **WHEN** `go test ./internal/domain/...` runs
- **THEN** a purity test fails if the domain package gains an internal or third-party import

### Requirement: AgentEngine seam

The domain SHALL define the `AgentEngine` interface with `Name()`, `RunGoal(ctx, GoalRequest) (<-chan TimelineEvent, error)`, `Resume(ctx, sessionID, UserInput) (<-chan TimelineEvent, error)`, and `Cancel(ctx, sessionID) error`, plus the `GoalRequest` and `UserInput` types they exchange.

#### Scenario: Engine integrates via the seam

- **WHEN** an engine adapter implements the four methods with the domain types
- **THEN** it compiles against the domain package alone and is a valid `AgentEngine`

#### Scenario: Goal run streams events

- **WHEN** `RunGoal` is called with a goal request
- **THEN** the returned channel delivers `TimelineEvent` values for that run's lifetime

### Requirement: Compactor seam

The domain SHALL define the `Compactor` interface (`ShouldCompact(ctx, ContextState) bool` and `Compact(ctx, []Message, TokenBudget) ([]Message, CompactionStats, error)`) plus the `Message`, `TokenBudget`, `ContextState`, and `CompactionStats` types.

#### Scenario: Middleware uses the seam

- **WHEN** an engine middleware converts its message state to `[]Message` and calls the compactor
- **THEN** it receives a compacted slice plus stats including tokens before and after and the strategy used

### Requirement: HookRunner seam

The domain SHALL define the `HookRunner` interface (`Run(ctx, HookEvent) (HookDecision, error)`) plus the `HookEvent` and `HookDecision` types, where a decision distinguishes allow, block, and warn-and-continue outcomes.

#### Scenario: Hook decision outcomes

- **WHEN** a hook decision is produced for an event
- **THEN** it expresses allow, block (with reason surfaced to the agent), or warn-and-continue

### Requirement: EventBus seam

The domain SHALL define the `EventBus` interface for publishing and subscribing to `TimelineEvent` values scoped by session topic.

#### Scenario: Bus implementation via the seam

- **WHEN** the eventbus package implements the interface
- **THEN** engines, hooks, and future persistence subscribe through the domain type without importing the implementation

### Requirement: Channel seam

The domain SHALL define the `Channel` interface (`Name()`, `Start(ctx, EventBus) error`, `Stop(ctx) error`) as the future extension seam for messaging plugins.

#### Scenario: Future channel plugs in

- **WHEN** a WhatsApp or Signal plugin is written later
- **THEN** it implements only this interface to participate in the daemon

### Requirement: Timeline event model

The domain SHALL define the `TimelineEvent` type carrying session identity, event kind, timestamp, and kind-specific payloads, with kinds covering agent steps, delegations, tool calls, hook decisions, and `CompactionTriggered` (tokens before/after, strategy).

#### Scenario: Compaction event carries stats

- **WHEN** compaction triggers during a run
- **THEN** a `CompactionTriggered` TimelineEvent is representable with tokens before, tokens after, and strategy

#### Scenario: Events are session-scoped

- **WHEN** a TimelineEvent is constructed
- **THEN** it identifies the session it belongs to
