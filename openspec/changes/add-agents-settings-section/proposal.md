# Proposal

## Why

Agent definitions (`~/.zbot/agents`) are live in the engine (delegation,
agent-named runs) but invisible in the UI — the user cannot see which
agents exist or what they can do. Settings needs an Agents section.

## What Changes

- Contract: `GET /api/agents` — read-only list from the registry snapshot
  (name, display_name, description, model, tools, max_depth, source file,
  load errors excluded — the registry's error list already reports them
  via the registry surface).
- Handler: server lists from the registry (the MCP-list pattern).
- Frontend: Settings gains an "Agents" section — read-only cards (name,
  description, model override, tool allowlist, delegation depth grant),
  empty state pointing at `~/.zbot/agents`.
- No mutation v1: agents are file-defined (the user owns the directory);
  management flows are a possible follow-up.

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `api-contract`: agents list endpoint (additive).
- `settings-shell`: Agents section (read-only v1).

## Impact

contracts/openapi.yaml + codegen, server handler, frontend Settings +
Agents component, tests each layer.
