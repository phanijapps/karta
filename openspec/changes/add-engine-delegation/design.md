# Design

## Context

The registry already loads Claude-style agent definitions (`config/agents/*.md`,
YAML frontmatter + markdown body as system prompt; `AgentSpec{Name,
SystemPrompt, Prompt, Model, Tools}`). The eino adapter ignores `req.Agent`
(F-25, pinned) and offers only builtin tools. `domain.EventDelegation` exists
unused. The operator's inspiration set: z-Bot sample agents (frontmatter with
description/model/skills/mcps), agentzero's execution model (async delegation,
per-actor tool inventory, delegated actors never escalate, durable state as
authority), and Claude-style definitions.

## Goals / Non-Goals

**Goals:**
- Delegation as an ordinary tool call: the model decides, children stream,
  results fold back — no new runtime machinery beyond bounded child runs.
- Per-agent tool inventory (the ward-guard idea): a child gets exactly its
  spec's allowlist; delegation itself is granted only by depth budget.
- The agent half of F-25 closed: agent-named runs use the agent's wiring.
- Every child step observable (constitution rule 4) as `EventDelegation`.

**Non-Goals:**
- Async/pending-children continuation (agentzero's full continuation model) —
  Karta's runs are bounded loops; v1 delegation is synchronous within the
  parent's step. The run-ownership follow-up owns async.
- skills[]/mcps[] frontmatter fields as tool sources (the tool planes don't
  feed the engine yet — recorded as follow-up when they do).
- Intent-analysis routers, reviewer quorums, ward templates as data — the
  discipline (bounded children, inventory, no self-escalation) is adopted,
  not the machinery.

## Decisions

1. **`delegate` is an adapter-registered tool, not a builtin.** It needs
   adapter state (models cache, store, bus, depth). Registration composes
   with the existing tools plane: the offered set = agent allowlist filtered,
   plus `delegate` when depth allows. Tool name `delegate_agent`; description
   embeds the configured agents' name+description pairs (the model's routing
   table).
2. **Child runs reuse the run loop, not RunGoal.** A child is a nested
   `run(...)` with its own exec record (parent_exec_id set), its own bounded
   step budget, its agent's system prompt, and depth+1. Sharing one loop
   implementation keeps semantics identical (the Send/SendStream lesson).
   Child events wrap as `EventDelegation` with the child's agent name and
   text; the child's final answer is the tool result payload.
3. **Depth is a grant, not a default.** An agent delegates only if
   `max_depth > its depth`; a config-level hard cap (default 2) bounds the
   tree even if specs over-grant. The delegate tool is simply absent from a
   non-granted run's tool set (the model cannot attempt what it cannot see).
4. **Cancellation propagates.** The child's ctx derives from the parent's
   run ctx; Cancel of the parent kills children. A child exceeding its
   bounds is a typed tool error, not a run failure.
5. **Strict decoding + description required for delegatable agents.** The
   delegate tool's description needs `description` (the when-to-delegate
   hint); agents without one are still loadable but not offered for
   delegation (they remain usable via `req.Agent`).
6. **Bootstrap seeds** (research-agent, reviewer-agent) ride the existing
   config bootstrap (files under the config tree on first run), marked as
   examples; the user's own samples are the template.

## Risks / Trade-offs

- [Runaway delegation trees] → depth grant + hard cap + per-run step bounds
  bound the total work; token budgets accumulate per execution.
- [Child failures] → typed tool results (the model sees and can retry or
  route around); never a silent swallow (rule 2.1's exclusion classes do not
  grow).
- [Snapshot staleness] → the registry snapshot is read once per run (as
  today); mid-run agent edits apply next run. Documented, consistent with
  the registry's reload model.
- [F-24 overlap] → delegation does not touch the cancels map beyond the
  parent's own entry (children are in-process, not registered) — the pinned
  characterization tests stay green unchanged.

## Migration Plan

Additive: new frontmatter fields are optional; no contract change unless the
delegation event payload needs fields beyond TimelineEvent's existing text
(it does not — agent name rides `Agent`). Rollback = revert.

## Open Questions

- Per-agent token budget enforcement (maxInput/maxOutput from the sample
  format) — defer until the engine has per-execution budget accounting
  (follow-up note in the ledger).
