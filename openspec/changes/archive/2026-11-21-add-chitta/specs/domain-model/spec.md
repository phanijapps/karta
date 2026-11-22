# Spec Delta

## ADDED Requirements

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
