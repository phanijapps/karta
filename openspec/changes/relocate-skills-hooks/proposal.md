# Proposal

## Why

The operator has ALREADY moved the skills tree to `~/.zbot/skills/`
(their layout directive: `~/.zbot/{skills,hooks,agents}` top-level,
matching the agentzero convention). The registry still loads
`config/skills/` (now empty) — every skill is currently invisible, the
resource index is starved, and intent analysis degrades. This change
makes the loader follow the layout, migrates any classic installs, and
tolerates raw operator skill files (sink frontmatter keys) so the
hand-moved set loads cleanly.

## What Changes

- **New roots**: skills load from `~/.zbot/skills/`, hooks from
  `~/.zbot/hooks/` (top-level, beside `agents/` — the operator's
  convention). `config/{skills,hooks}/` are no longer read.
- **One-time migration** at bootstrap, non-destructive and
  hand-move-aware: entries under `config/skills/` / `config/hooks/`
  move into the top-level roots ONLY when the destination does not
  already have them (the operator's hand-moved state — top-level
  populated, config empty — is already done; classic installs migrate;
  conflicts never overwrite).
- **Loader tolerance**: SKILL.md frontmatter grows documented SINK keys
  (`license`, `metadata`, `allowed-tools`) — decoded and ignored so raw
  operator skills (duckduckgo-search, pdf, spec-builder, …) load
  without load errors. The name/description rules stay strict.
- **Seeds**: the five seeded example skills target the new root
  (persona/intent prompts stay in `config/prompts/` — prompts are
  daemon config, not user skills).
- Docs/specs: constitution rule 1 tree text, config-plane spec
  (relocation/bootstrap/self-heal lists), AGENTS.md notes.

## Capabilities

### Modified Capabilities
- `config-plane`: the home tree — skills/hooks move top-level; the
  seeding + self-heal + relocation clauses track the new roots.
- `skill-management`: skills load from `~/.zbot/skills/`; sink
  frontmatter keys tolerated.

## Impact

`internal/config` (Paths, Subdirs, ensureDirs, migration, seeder),
`internal/registry` (Dirs doc, skill loader sink keys), `cmd/zbotd`
(LoadAll/Watch wiring), constitution + config-plane spec, docs. No
contract, engine, or frontend changes.
