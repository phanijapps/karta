# Proposal

## Why

Karta's goal runs are single-agent: one bounded loop over one model with
tools. The architecture promises multi-agent work (`domain.EventDelegation`
exists as a Timeline event kind; the registry already loads agent
definitions), but nothing spawns or folds back sub-agents. Delegation is the
last missing piece of the agent loop — and Phase 1 of
[`docs/product/backlog.md`](../../../docs/product/backlog.md).

## What Changes

- A delegation tool in the engine's tool plane: a goal run may start a child
  `RunGoal` under a named agent definition (from the registry), with its own
  bounded loop.
- Child runs stream into the parent's Timeline as `EventDelegation` events
  (constitution rule 4: delegation emits); the child's final result folds
  back into the parent as a tool result.
- Depth/breadth limits (max children per run, max depth) fixed in config,
  not code constants alone.
- A `contracttest` scenario for delegation semantics (child lifecycle,
  cancellation propagation to children, event emission) joins the conformance
  suite — any future engine must honor it.
- Contract: sessions/Timeline already model executions; any new surface
  (e.g. listing a run's delegations) starts in `contracts/openapi.yaml`.

## Capabilities

### New Capabilities

- `engine-delegation`: sub-agent delegation within goal runs — lifecycle,
  limits, event emission, result folding.

### Modified Capabilities

- `agent-engine`: the engine contract gains delegation semantics (the
  conformance suite grows a delegation scenario).

## Impact

`internal/engine/eino` (delegation tool + child-run wiring), `internal/registry`
(agent definitions already present), `internal/domain` (delegation event
payload shape), `contracttest`, `contracts/openapi.yaml` + codegen if any
read surface is added. Follow-up awareness: the run-ownership follow-up
(`backend-engine-run-ownership` findings in the archived
backend-code-quality-refactor ledger) touches the same adapter regions —
land it first or sequence explicitly.
