# Proposal

## Why

Karta's goal runs are single-agent: one bounded loop, one model, one tool
set — even though the registry loads agent definitions, `domain.EventDelegation`
exists as an event kind, and the product is specced as a multi-agent system.
Delegation is the last missing piece of the agent loop (backlog Phase 1).
Direction set by the operator: Claude-style agent definitions (one markdown
file, YAML frontmatter + system-prompt body — the format the registry already
parses), with the delegation discipline of the agentzero-style execution model
(per-agent tool inventory, bounded depth, delegated agents never self-escalate)
and the ward-guard idea (children return bounded requests instead of mutating
durable scope; they act, the root owns the workspace).

## What Changes

- **Agent definitions (Claude style, extended):** `~/.zbot/config/agents/*.md`
  — frontmatter gains `description` (when-to-delegate hint surfaced to the
  model in the delegate tool description) and `max_depth` (delegation budget;
  default 0 = may not delegate). Body = system prompt. Existing yaml specs
  keep working; strict decoding stays.
- **Agent-forwarded runs (closes the agent half of F-25):** a goal run with
  `req.Agent` set resolves that agent (system prompt, qualified model override,
  tool allowlist) instead of the fixed built-in prompt. Empty agent keeps the
  default wiring.
- **The `delegate` tool:** an engine-registered tool (not a builtin — it needs
  adapter state) offered to runs whose depth budget allows it. Arguments:
  `{agent, task}`. Execution spawns a CHILD bounded run under the named
  agent's definition with its own tool inventory (allowlist ∩ parent's plane,
  minus `delegate` unless the child's max_depth allows), streams the child's
  lifecycle as `EventDelegation` events into the parent's channel (child
  steps visible in the Timeline), and folds the child's final answer back as
  the tool result. Unknown agent names are typed tool errors the model can
  see (listing configured agents).
- **Depth/breadth bounds:** root depth 0; a child may delegate only if its
  own spec's max_depth exceeds its depth. A global cap (config, default 2)
  bounds the tree regardless of specs. Cancellation propagates parent → child.
- **Conformance:** a delegation scenario joins the `contracttest` suite —
  spawn, delegation events, result folding, depth refusal — so every future
  engine must honor delegation semantics.
- **Bootstrap defaults:** two example agents (research, reviewer) seeded into
  the default config tree at first run so delegation is usable out of the box
  (config-tree files, not hardcoded code — the zero-hardcoded rule holds).
- Engine-plane thinking display and per-request thinking-effort stay in their
  recorded follow-ups. The run-ownership follow-up's remaining items (F-23
  detach, F-24 overlap, F-26 cache key) are untouched.

## Capabilities

### New Capabilities

- `engine-delegation`: sub-agent delegation within goal runs — frontmatter
  definitions, per-agent tool inventory, bounded depth, delegation events,
  result folding, cancellation propagation.

### Modified Capabilities

- `agent-engine`: the engine contract gains delegation semantics (conformance
  scenario) and agent-forwarded runs.

## Impact

`internal/registry` (AgentSpec fields), `internal/domain` (delegation event
payload), `internal/engine/eino` (agent resolution + delegate tool + child
runs), `internal/engine/contracttest` (scenario), `internal/config`
(bootstrap seeds, depth cap), `contracts/openapi.yaml` only if the delegation
event payload needs new fields (else zero contract change). Frontend: child
events render in the Timeline (existing event stream handles new kinds).
