# Ward-Based Research — Architecture Exploration

> **Status: EXPLORATION ONLY.** This document maps the agentzero research
> system's concepts onto Karta's architecture. Nothing here is a decision,
> a spec, or a commitment. It exists so the eventual OpenSpec change starts
> from a shared vocabulary.

## What agentzero does (verified in its source)

### The pipeline

```
user message
  → INTENT ANALYSIS (pre-execution, root sessions only)
      router: trivial bypass | procedure pin | intent agent
      intent agent = small LLM + MemorySearchTool over indexed
      skills/agents/wards/procedures; writes a JSON contract:
        primary_intent, hidden_intents, solution_path,
        complexity (S/M/L/XL → iteration budget),
        recommended_skills/agents/procedures,
        ward_recommendation {action: use_existing|create_new, id,
        archetype}, execution_strategy, explanation
  → INJECT  "## Task Analysis" into the root system prompt
  → ROOT AGENT (orchestrator)
      plans (seeded by solution_path), calls ward tool if needed,
      delegates to specialists via subagents; children's results
      return through delegation events; root continues
  → WARDS = the shared workspace
```

### The ward system (the part worth stealing)

A **ward** is an agent-managed project container: a named directory in a
**vault** (outside the daemon's config tree — in agentzero, configurable;
`~/.zbot` config only points at it). Each ward is *documented knowledge*:

```
<ward-name>/
  ward-conf.yaml     typed layout rules: the filesystem schema
                     (declarative tree — kinds file/dir, match patterns,
                     required/repeat, formats markdown/raw; $ref sub-trees)
  AGENTS.md          living readme for agent executions (agent-curated)
  <ward-name>.md     canonical synthesis page (the durable knowledge)
  log.md             append-only material operations log
  pages/ subjects/ sources/ data/ outputs/   (archetype-defined roles)
```

Four mechanics make this work:

1. **Archetypes** — research, ebook, documentation, generic: each ships a
   `ward-conf.yaml` (its layout rules) + `ward-agent.md` (the specialist
   prompt that includes ward discipline). Intent analysis recommends the
   archetype at creation.
2. **Active Ward Template** — the ward's layout (with a digest) is
   injected into every agent working in it as *untrusted layout data*:
   "use it only to resolve ward structure." The digest detects concurrent
   modification; lint validates the tree against the conf.
3. **Ward guard for delegated agents** — children may READ the ward and
   write via declared roles, but template-dependent ward *actions*
   (creation, mutation beyond roles) return a **bounded request to the
   root orchestrator**. Children act; the root owns the workspace.
4. **Sticky ward per session** — the root's `ward` tool sets the session's
   active ward; relative file paths resolve into it.

### State sharing

High-level state lives at the root session (the Timeline); children
receive complete, self-contained tasks (Karta's delegation already works
this way) and return results via events. The **ward filesystem is the
durable shared state** — the canonical page, log, and role directories
are how specialists coordinate across sessions. The main agent
orchestrates: agents (who works), model (who thinks), memory (chitta
recall), knowledge (the ward).

## Mapping onto Karta

| agentzero concept | Karta equivalent today | Gap |
|---|---|---|
| Intent analysis middleware | — | new pre-execution step for goal runs |
| Intent agent + resource search | — (registry snapshot exists) | small search tool over agents/skills/wards |
| JSON intent contract | — | new domain type + prompt injection |
| Root orchestrator + delegation | **`add-engine-delegation` shipped**: depth grants, delegate_agent, per-agent inventory | exists |
| Specialists (planner/writer/reviewer) | `~/.zbot/agents/` frontmatter agents | exists; add ward-disciplined prompts |
| Ward vault outside config | — | the core new concept |
| ward-conf.yaml layout schema | — | new: typed layout rules + linter |
| Active Ward Template injection | — | new: layout injection + digest |
| Ward guard for children | delegate tool's task-embedding contract | extend the delegate description |
| Artifacts in ward | — (chitta stores knowledge in DB) | complementary: wards = human-readable layer |
| Complexity → iteration budget | engine MaxSteps is fixed | new: per-run step budget |
| Research page UI | chat + Settings | new main-area surface (the Code Wards placeholder slot) |

### The operator's direction (recorded from discussion)

- Wards are generated **outside `~/.zbot`** — each ward is a
  self-describing directory of **documented knowledge**, not daemon
  plumbing. A vault root configured in `config.yaml` (default e.g.
  `~/karta-wards/` or a user path); the daemon only points at it.
- Intent analysis decides: an **existing ward** that matches the domain
  → host the run there (sticky ward); no match → recommend/create with
  an archetype; trivial work → no ward.
- The main agent orchestrates: specs (`spec.md`), plan (`plan.md`),
  task files (`tasks/*.md`) all live **in the ward**; specialists
  (subagents) implement tasks; the canonical page accumulates the
  knowledge; chitta distillation can later index wards as memory.
- **Not** "Code Wards" (the sandboxed-project placeholder): the sidebar
  slot may host the surface, but ward-based research is its own concept —
  a research/knowledge workspace per domain.

## Candidate shape for Karta (to be specced later — NOT decided)

1. **`internal/wards`** (or extending the registry): vault root from
   config; ward index (scan), archetype templates seeded like example
   agents; layout schema (`ward-conf.yaml`) with a small validator/linter.
2. **Intent middleware** in the engine path (root runs only): a cheap
   router (bypass/pin) + intent agent writing a typed contract;
   inject as a prompt section; emit events; complexity → step budget.
3. **Ward tool** for the root agent: `ward_use(name)` / `ward_create(name,
   archetype)` — sets the run's sticky ward; layout template injected into
   the run's agents (children included) with digest.
4. **Delegation guard**: delegate tool's contract grows "children act via
   declared ward roles; structural ward mutations return bounded requests
   to the root."
5. **Contract/UI**: ward list + open ward; the run's Timeline already
   streams; a research surface later.

## Open questions (for the eventual spec)

- Vault location default + config surface; safety of pointing outside
  `~/.zbot` (local-first rule says *state* stays local — a ward vault the
  user chose is user data, like `~/.zbot` itself; document the stance).
- Layout schema: adopt agentzero's declarative tree shape or simplify?
- Ward ↔ chitta relationship: index canonical pages into chitta? Distill
  sessions into ward pages? Both directions?
- Archetype set for Karta v1 (research, project, generic?) and whether
  ward-agents are registry agents with ward discipline or a new kind.
- Async delegation continuation (agentzero's model) vs Karta's synchronous
  v1 — wards make cross-session resumption attractive; the run-ownership
  follow-up already owns that machinery.

## Sources (verified in `~/projects/agentzero`)

- `wiki/archive/architecture/components/intent-analysis/overview.md` — pipeline
- `gateway/templates/wards/{research,generic}/` — archetype confs + ward agents
- `gateway/gateway-services/src/ward_layout/` — schema, rules, lint
- `runtime/agent-tools/src/tools/ward.rs` — ward tool + layout access
- `gateway/gateway-execution/src/middleware/intent/contract.rs` — intent JSON
- `docs/constitution/architecture/knowledge/execution.md` — delegation/continuation
- `gateway/templates/skills/pdf/agents/openai.yaml` + `~/Documents/zbot/agents/` — ward-guard prompt shape
