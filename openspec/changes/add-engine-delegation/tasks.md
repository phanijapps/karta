# Tasks

## 1. Registry: extended frontmatter

- [ ] 1.1 Extend `AgentSpec` with `description`, `display_name`, and
  `max_depth` (strict YAML decode; zero values = unset). Verify: registry
  table tests — frontmatter .md with the new fields loads; unknown field is
  a typed load error; plain yaml specs unchanged.
- [ ] 1.2 Seed bootstrap example agents (research-agent, reviewer-agent) via
  the config bootstrap on first run, marked as examples. Verify: fresh home
  bootstrap test — files exist under config/agents, snapshot loads them.

## 2. Engine: agent-forwarded runs + delegate tool

- [ ] 2.1 Agent resolution in the adapter: `req.Agent` resolves via the
  snapshot (system prompt, model override when the request names none, tool
  allowlist); unknown names fail typed listing configured agents (closes
  the agent half of F-25; the pinned characterization comment updates to
  name only the Context field as unwired). Verify: adapter tests for named,
  model-overridden, allowlisted, and unknown-agent runs.
- [ ] 2.2 The `delegate_agent` tool (adapter-registered; offered only when
  depth grant allows; description lists delegatable agents + descriptions):
  spawns a child run (nested loop, own execution record with parent linkage,
  agent inventory ∩ tool plane, no delegate unless child grant allows,
  config hard cap), emits the child's steps as `EventDelegation` events,
  folds the final answer back as the tool result; unknown target = typed
  tool error listing agents. Verify: adapter tests — fold-back, depth
  refusal (tool absent), unknown target, cancellation propagation, hard-cap
  enforcement.
- [ ] 2.3 Conformance suite gains the delegation scenario group. Verify:
  suite green against the real adapter; a deliberately broken fake (drops
  child events) fails it.

## 3. Config + observability

- [ ] 3.1 Config: delegation hard cap (default 2) in the config tree with
  validation; docs note in go-rules/backend AGENTS untouched (no rule
  change). Verify: config load tests incl. invalid values.
- [ ] 3.2 Timeline: delegation events render (agent name on the event);
  store execution linkage (parent_exec_id) round-trips. Verify: store +
  event-shape tests.

## 4. Review and hardening

- [ ] 4.1 Run the read-only `zbot-reviewer` subagent over the change scope,
  artifacts, and the five constitution boundaries (durable-before-volatile
  on child events especially); fix every P0/P1, fix P2s or record why not.
  Verify: reviewer report shows zero open P0/P1.
- [ ] 4.2 Re-run `task verify` green after the final edits. Verify: exits
  zero.
