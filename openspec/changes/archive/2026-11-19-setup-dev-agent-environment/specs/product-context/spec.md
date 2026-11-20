# Spec Delta

## Purpose

Establishes `docs/product/product-context.md` as the single maintained source of product truth that grounds agent product-level decisions, alongside the preserved founding brief in `docs/architecture/`.

## ADDED Requirements

### Requirement: Product context document

The repository SHALL maintain `docs/product/product-context.md` covering: what z-Bot is (a local-first, autonomous, goal-oriented multi-agent desktop system with a Go daemon and multiple lightweight clients), the target users, the core product concepts (daemon/client split, the swappable agent engine seam, compaction, the Timeline Tree, Code Wards, the filesystem control plane, and local-first memory), and explicit non-goals.

#### Scenario: Product-level decision needs grounding

- **WHEN** an agent makes a decision that affects product behavior or scope
- **THEN** it consults `docs/product/product-context.md` as the source of product truth

#### Scenario: Document coverage check

- **WHEN** the product context document is inspected
- **THEN** it contains sections for the product vision, target users, core concepts, and non-goals

### Requirement: Founding brief is preserved in-repo

The architecture brief that founded the project SHALL be preserved verbatim at `docs/architecture/architecture-brief.md`, and `product-context.md` SHALL be coherent with it. Where later evolution makes the two disagree on a product matter, `product-context.md` SHALL win; the brief remains the historical founding record.

#### Scenario: Disagreement between brief and product context

- **WHEN** a product decision depends on a point where the brief and `product-context.md` differ after later evolution
- **THEN** `product-context.md` is authoritative for the product decision

### Requirement: Product context is maintained with the product

A change that alters product-level behavior or scope SHALL update `docs/product/product-context.md` within the same change.

#### Scenario: Capability changes product behavior

- **WHEN** a change adds or alters user-visible product behavior, concepts, or non-goals
- **THEN** `product-context.md` is updated in that same change

#### Scenario: Pure internal refactor

- **WHEN** a change alters only internal implementation with no product-level effect
- **THEN** `product-context.md` is not required to change
