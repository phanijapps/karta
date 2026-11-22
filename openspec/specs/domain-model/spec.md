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

### Requirement: Memory and knowledge-graph seams

The domain SHALL define the `MemoryStore` and `KnowledgeGraph` interfaces
together with the pure record types they exchange, as the seams every
chitta consumer depends on, implemented outside the domain package (by
the chitta capability), using `context.Context` for cancellation,
mirroring the existing `Compactor`/`AgentEngine` seam style.

#### Scenario: Interfaces carry the record types
- **WHEN** the seam interfaces are compiled
- **THEN** their method signatures reference only the pure contract record types and `context.Context`

### Requirement: Trusted host operations on the seam

The seams SHALL expose trusted host operations that are never
agent-facing: distiller writes carrying host-stamped provenance,
embedding writes, and query embedding injection.

#### Scenario: Distiller write is not a tool
- **WHEN** a future distiller writes observations through the seam
- **THEN** provenance is host-stamped (`source: distiller`) and no agent-facing tool exposes the operation

#### Scenario: Consumers depend only on the seam
- **WHEN** a package outside the chitta capability needs to save or recall memories
- **THEN** it does so against the `domain.MemoryStore` / `domain.KnowledgeGraph` interfaces without importing the chitta implementation package

#### Scenario: Domain purity holds
- **WHEN** domain purity checks run over `internal/domain`
- **THEN** the new seam types introduce zero imports from other internal packages and no storage-driver types leak into the interfaces
