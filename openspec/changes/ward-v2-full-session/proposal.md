# Proposal

## Why

The operator's direction: wards should run like agentzero's full
sessions — with their own persistent selectable agents ("wardens"),
a richer ward layout, and the two planes the NVDA post-mortem proved
missing (execution + retrieval). The resources exist: agentzero's
ward shape (~/Documents/zbot/wards/*), its persona set
(~/Documents/zbot/config/agent/{SOUL,OS,INSTRUCTIONS}.md), its
ward-agent template, and a configured keyless search MCP
(ddg-search via uvx).

## What Changes

- **Ward v2 layout** (every ward, template-seeded + linted):
  ```
  .zbot/      config.yaml — name, description, last_used, agents, skills
  wiki/       LLM wiki: ward-specific timebound knowledge (dated entries)
  reports/    outputs (PDF/HTML/MD…) organized by run date
  apps/       the ward's code, best-practices organized (src/, data/, …)
  AGENTS.md   ward rules and learnings (the constitution)
  ```
  Additive migration for existing wards (dirs created, nothing moved);
  the v1 roles (canonical, spec/plan, sources) keep working.
- **Warden sessions**: each ward's persistent research session —
  auto-created on demand (`ward-<name>`), bound to the ward, listed in
  the sidebar under Ward Bots, selectable like any session. The ward's
  `.zbot/config.yaml` records identity + last_used (touched on entry).
- **Persona seeding**: SOUL/OS/INSTRUCTIONS seeded from the operator's
  agentzero set (verbatim, user-editable); the research protocol keeps
  its slot.
- **Execution plane**: a shell tool for hosted runs — `ward_shell` —
  cwd-pinned to the active ward (agentzero's proven shape: bash, 120s
  default timeout, output caps, env scrub; refused without an active
  ward). Root and children both get it inside a hosted run.
- **Retrieval plane**: the MCP execution bridge — the daemon spawns
  configured stdio MCP servers (ddg-search first), probes their tools,
  and mounts them on hosted runs' tool planes (search/fetch become
  real). Failures degrade non-fatally (tools absent, event says why).
- **Active Ward packet v2**: the ward's AGENTS.md + layout now also
  carries the wiki/reports/apps discipline (where outputs go, how
  wiki entries are dated, apps organization) so runs put artifacts in
  the right homes.

## Capabilities

### New Capabilities
- `ward-execution`: the shell tool (ward-scoped, bounded).
- `mcp-execution`: stdio MCP servers as live tool planes.

### Modified Capabilities
- `ward-vault`: the v2 layout, .zbot config, additive migration.
- `chat-modes`: warden sessions (per-ward persistent research
  sessions, selectable).
- `system-prompt`: the seeded persona trio.

## Impact

`internal/wards` (v2 layout + config), `internal/wardtools` (shell
tool, packet v2), `internal/mcpclient` (execution bridge — stdio
client), `internal/engine/eino` (mount MCP + shell tools on hosted
runs), `internal/chat`+`server` (warden sessions), `internal/config`
(persona seeds), frontend (warden sessions in the sidebar — no
structural change), contracts (ward detail carries v2 fields where
needed).
