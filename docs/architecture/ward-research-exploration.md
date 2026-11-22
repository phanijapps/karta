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
| Intent agent + resource search | — (registry snapshot + chitta embedder exist) | semantic index over skills/MCP tools/agents (chitta-kind records; embedding via the existing ONNX seam) |
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
  task files (`tasks/*.md`) all live **in the ward** — and the
  spec→plan→tasks **flow itself is executed via SKILLS**, not hardcoded
  engine machinery: the orchestrator loads the relevant skills (a spec
  skill knows how to elicit/write `spec.md`; a plan skill turns a spec
  into `plan.md`; a task skill decomposes the plan into `tasks/*.md`),
  and the ward's conf declares where their outputs land. Skills stay the
  reusable-knowledge plane; the ward is where their outputs become
  documented knowledge; specialists (subagents) implement the tasks; the
  canonical page accumulates the knowledge; chitta distillation can
  later index wards as memory.
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
3. **MCP tool loading + ward tool** for the root agent: enabled MCPs'
   tools become per-run loadable (intent-recommended subset by default);
   `ward_use(name)` / `ward_create(name, archetype)` sets the sticky
   ward; layout template injected (children included) with digest.
   Programmatic orchestration: `list_tools`/`call_tool` over the loaded
   MCP set — chat and goal runs share the mechanism. Local ward-scoped
   execution (shell/python via uv/node) first; Docker sandbox operator
   experimental, config-gated.
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
- Skill-driven spec→plan→tasks: are the flow skills shipped as seeded
  examples (like the example agents) or marketplace items? Do ward
  confs reference required skills, and does the intent analysis
  recommend them from the skills index?
- Async delegation continuation (agentzero's model) vs Karta's synchronous
  v1 — wards make cross-session resumption attractive; the run-ownership
  follow-up already owns that machinery.

## Tool plane direction (operator refinement — MCP-first, recorded)

The operator's refinement of the execution strategy:

- **MCPs are the primary dynamic tool plane.** Karta already manages
  MCPs (CRUD, enablement, prober that discovers each server's tools
  with names+descriptions). The gap is RUNTIME tool loading: today MCP
  tools are probed for display but not offered to runs. The direction:
  enabled MCPs' tools become loadable per-run — intent analysis
  recommends which MCPs to activate (from the probed tool index), the
  orchestrator/chat loads exactly those tools, keeping the default
  offering light.
- **Dynamic during intent analysis — and in chat too.** The same
  discovery+load mechanism serves goal runs (ward-hosted research) and
  quick chat: a `list_available_tools`-style capability (over the
  enabled MCP index) lets the model see what exists and orchestrate
  programmatic tool calls (call an MCP tool by name with args), instead
  of Karta hardwiring tool inventories per surface.
- **Semantic discovery over the resource index (operator refinement).**
  Skills, MCP tools, and agents are INDEXED with embeddings — chitta's
  local ONNX embedder (EmbedPassages/EmbedQuery seam, already proven in
  recalleval) embeds each resource's name+description (skill SKILL.md
  frontmatter; each MCP server's probed tool list; agent descriptions;
  ward canonical pages later) into a resource index, and intent
  analysis recalls semantically (Recall-style vector+keyword fusion)
  instead of receiving full listings. The listing stays the fallback
  (small registries, cold cache); the index scales discovery when
  rosters grow. Key shape decision for the spec: resources as chitta
  records of a reserved kind (reuse Remember/Recall wholesale, filtered
  by kind) vs a dedicated index collection — leaning chitta-kind to
  avoid a second embedding pipeline; the recall floor findings (F-30:
  BM25-dominated floors) must be re-verified for this corpus.
- **Docker sandbox stays EXPERIMENTAL.** The eino-ext DockerSandbox
  operator is the isolation upgrade path (Code Wards convergence) but
  ships behind a config flag, off by default. First test target: the
  LOCAL HOST OS — a local ward-scoped operator (direct exec, cwd=ward,
  guardrails) proves the pipeline; Docker wraps the same Operator
  interface later when hardened.

## Tool-execution findings (investigated for slice 3, recorded here)

- **eino-ext main ships `components/tool/commandline`** (not in our
  pinned version; separate go module): an `Operator` interface
  (RunCommand([]string) → stdout/stderr/exit — direct exec, no shell
  string), `NewPyExecutor` (temp .py + configured command), and
  `NewStrReplaceEditor`; the only shipped operator is DockerSandbox
  (python:3.9-slim, resource limits — Docker required). Karta's
  leverage: implement a ward-scoped LOCAL Operator (cwd = active ward,
  guardrails, uv python) and run the upstream tools on it;
  DockerSandbox becomes the optional isolation upgrade (Code Wards
  convergence). Tool imports live in Karta's tool plane, not
  engine/eino/. Other tool components on main worth watching:
  httprequest, duckduckgo/bing/google search, wikipedia,
  sequentialthinking.
- **agentzero's execution family** (`tools/execution/`: shell.rs with a
  blocked-command list + timeout + platform shell; write_file/edit_file;
  ward_cwd.rs scoping cwd to the ACTIVE WARD with `~/Documents/zbot/
  wards/<id>` fallback — the same Documents convention as our default).
  Port the *shape* (cwd scoping, guardrails, caps) in Go.
- **Python via uv** per ward: `uv venv` + `uv pip install` + `uv run
  python` (agentzero skills already call `uv pip install` inline); ward
  venv lives inside the ward; absent uv → typed tool error naming the
  install path. **Node** via `node`/`npx` on demand, same shape.

## Operator's intent-analysis prompt reference (`~/Documents/zbot/config/`)

The operator runs agentzero with a tuned prompt set — the intent prompt
(`config/intent-analysis-prompt.md`) and agent instructions are
**reference material for slice 2** (assess quality, adopt, or replace —
not blindly copy):

**Intent prompt strengths worth keeping:**
- Skills vs agents are explicitly distinguished (load_skill vs
  delegate_to_agent — "never mix them"), with a hard "recommend from
  the listed resources only" rule (no hallucinated names)
- **Ward naming discipline**: wards are REUSABLE domain categories
  ("financial-analysis" good; "amd-stock-analysis" bad) — never
  task-specific; existing-ward matching with exact-name reuse before
  create-new ("never invent a near-duplicate")
- **Approach gating**: "simple" vs "graph" with concrete criteria
  (graph only for multi-agent/pipeline/spec-plan-artifact work) and an
  anti-pattern callout ("do NOT choose graph merely because the answer
  needs current data or research")
- Flat JSON contract (no envelope), runtime-supplied schema, no tools/
  browsing during analysis

**Differences from agentzero's contract worth noting for Karta:** the
operator's version drops complexity/iteration-budget and solution_path;
agentzero's has them. Karta's spec should decide deliberately.

**Supporting prompt family** (`config/agent-prompts/`, `config/agent/`):
- `first-turn-protocol.md` — the **fast-path override** (approach=simple
  → skip ward/planner/plan-reading entirely) + the one-action-per-turn
  agent loop + graph first-actions (enter ward → planner → read
  specs/plan.md → delegate steps in order)
- `INSTRUCTIONS.md` — execution modes keyed to intent: warm path (ward
  matches → ONE delegation to the ward-agent), cold path (no ward →
  planner-agent first, spec-driven plan in specs/plan.md, step-by-step
  delegation); plan.md re-read on every continuation; completion =
  synthesize artifacts + respond
- `OS.md` — platform pack (shell syntax, python invocation, ward paths
  `~/Documents/zbot/wards/{ward}/`) — the template for Karta's
  per-OS runtime info injection
- `chat-instructions.md`, `SOUL.md`, `tooling-skills.md` — chat protocol
  (memory-first answering), persona, and the tool-family contract
  (shell/write_file/edit_file/update_plan discipline) for reference

These live at `~/Documents/zbot/config/` — the exploration treats them
as the operator's quality bar for slice 2's prompts.

## Sources (verified in `~/projects/agentzero`)

- `wiki/archive/architecture/components/intent-analysis/overview.md` — pipeline
- `gateway/templates/wards/{research,generic}/` — archetype confs + ward agents
- `gateway/gateway-services/src/ward_layout/` — schema, rules, lint
- `runtime/agent-tools/src/tools/ward.rs` — ward tool + layout access
- `gateway/gateway-execution/src/middleware/intent/contract.rs` — intent JSON
- `docs/constitution/architecture/knowledge/execution.md` — delegation/continuation
- `gateway/templates/skills/pdf/agents/openai.yaml` + `~/Documents/zbot/agents/` — ward-guard prompt shape
- `~/Documents/zbot/config/intent-analysis-prompt.md` + `agent-prompts/*` + `agent/INSTRUCTIONS.md` — the operator's tuned prompt set (slice 2 reference)
