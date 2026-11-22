# Proposal

## Why

Ward-based research (see docs/architecture/ward-research-exploration.md)
needs its foundation: a vault of wards outside `~/.zbot`, each a
self-describing directory of documented knowledge with a typed layout
schema. Everything else — intent analysis recommending wards, the ward
tool, hosting runs in wards — builds on this slice. Standalone value:
wards exist, validate, and are visible in the UI.

## What Changes

- **Vault config**: `wards.vault_root` in config.yaml (default
  `~/karta-wards` — OUTSIDE `~/.zbot` by design: wards are user data /
  documented knowledge, not daemon state; the daemon only points at the
  path). Missing dir = created empty on bootstrap (like the config tree).
- **`internal/wards` package**: vault scan (ward index), ward-conf.yaml
  schema parse (declarative tree: kind file/dir, match, required/repeat,
  format markdown/raw, $ref), layout linter (validate a ward's tree
  against its conf), archetype registry (generic + research seeded as
  templates on first bootstrap of the vault — user-owned after).
- **Contract (contract-first)**: `GET /api/wards` (list: name, archetype,
  lint status, canonical-page title when present) and
  `GET /api/wards/{name}` (the parsed layout tree). Read-only v1 —
  wards are created by the agent (slice 3) or by hand.
- **UI**: the Code Wards sidebar placeholder becomes **Wards** — list of
  ward cards (name, archetype badge, lint state, canonical title), empty
  state pointing at the vault path.
- OUT of scope: intent analysis, ward tools/agents, spec→plan→tasks
  skills, chitta indexing — slices 2-5 in the backlog.

## Capabilities

### New Capabilities
- `ward-vault`: wards outside ~/.zbot — vault config, layout schema,
  linter, archetype templates, read-only listing.

### Modified Capabilities
- `api-contract`: wards list/read endpoints (additive).
- `settings-shell` (or nav): the Wards surface replaces the Code Wards
  placeholder.

## Impact

internal/config (vault root), new internal/wards, contracts + codegen,
server handlers, frontend sidebar + wards page, docs. Local-first: the
vault is user-chosen local storage — the rule's intent (no cloud, no
remote state) holds; the constitution's `~/.zbot` text gets a scoped
exception entry (like the keyring one) for the user-configured vault.
