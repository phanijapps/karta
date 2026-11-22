# Proposal

## Why

The operator wants agent definitions to live at `~/.zbot/agents` — a
top-level home for the multi-agent roster (matching where delegation
definitions conceptually belong), not nested under `config/`.

## What Changes

- `Paths.AgentsDir` (`~/.zbot/agents`) becomes the canonical agents
  directory; the registry (load + watch) and bootstrap seeding use it.
- One-time migration on bootstrap: existing `config/agents` files move to
  the new directory when it is absent (user-owned files never overwritten).
- Docs and specs updated in the same change (root AGENTS.md config-tree
  rule, registry/config package docs, config-registry + agent-engine spec
  text).

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `config-registry`: agent specs load from `~/.zbot/agents` (was
  `config/agents`); legacy location migrated once.
- `agent-engine`: the registry-agent reference path text updates.

## Impact

`internal/config` (Paths + bootstrap seed/migrate), `cmd/zbotd` wiring,
docs, specs. No contract or engine changes.
