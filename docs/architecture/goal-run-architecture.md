# Goal Run Architecture (Research Mode)

Status: the engine loop and delegation are in the code. Intent
analysis, skill provisioning, and ward hosting are planned in OpenSpec
changes `add-intent-analysis` (slice 2) and `add-ward-hosting`
(slice 3). The section "Current and planned parts" tells you which is
which. Dashed boxes in the diagrams show planned parts.

Written in ASD Simplified Technical English (ASD-STE-100).

## Technical names

This document uses these technical names: agent, archetype, chitta,
client, daemon, daemon database, delegate tool, delegation, eventbus,
goal, intent analysis, Karta, memory, knowledge graph, provider,
resource index, run, session, skill, SSE, step budget, sticky ward,
system prompt, Timeline, tool, ward, ward constitution, ward vault.

## Purpose

The goal run does autonomous work. The user gives one goal. The daemon
analyzes the intent, selects the resources, and runs a bounded loop of
steps until the goal is complete or the budget is used.

The goal run is the research mode of Karta. A ward can host the run.
The ward is a small specialized bot, for example "stock analysis" or
"india analysis". When a run enters a ward, the run uses the
constitution of that ward.

The goal run uses memory and knowledge. It recalls both chitta domains:
memory records and the knowledge graph.

## Parts

| Part | Job |
|---|---|
| Client | It sends the goal and shows the Timeline. |
| Daemon (`zbotd`) | It receives the goal and starts the run. |
| Intent middleware | It analyzes the goal before the run starts. |
| Intent agent | One small-model call. It writes a typed contract. |
| Resource index | chitta records of kind skill, MCP tool, and agent. |
| Ward vault | Wards outside `~/.zbot`. User data, read-only for the daemon. |
| Engine seam | `domain.AgentEngine`. The only engine interface. |
| eino adapter | The bounded loop. The only code that imports eino. |
| Agent roster | Agent files in `~/.zbot/agents`. The root delegates to them. |
| System prompt | The same composed sections as the chat, plus dynamic sections. |
| eventbus | It takes each event to the Timeline and to all clients. |

## Phase 1: intent analysis (before the run)

The intent analysis runs before the engine starts. It has these steps:

1. A deterministic router checks the goal. Trivial goals bypass the
   analysis. Examples: a greeting, or one quick fact.
2. For all other goals, the daemon makes one small-model call. The
   prompt comes from the file `~/.zbot/config/prompts/intent-
   analysis.md`. The user owns this file. The daemon seeds it from the
   operator version, with Karta names.
3. The runtime injects three lists into the prompt slots:
   `{{.Skills}}` from the resource index, `{{.Agents}}` from the
   resource index, `{{.Wards}}` from the ward vault index.
4. The intent agent returns one flat JSON object. It does not call
   tools. It does not browse. The runtime supplies the schema.
5. The daemon validates the contract against the index. A name that is
   not in the index is removed, and the removal is recorded as a
   warning event. The daemon never runs a hallucinated name.
6. If the JSON does not parse, the daemon retries once. If it fails
   again, the run continues without analysis. The event says
   `parse_failed`.

The contract holds:

- `primary_intent`, `hidden_intents`
- `solution_path` (it seeds the plan)
- `complexity`: S, M, L, or XL. The complexity sets the step budget:
  4, 8, 16, or 24 steps.
- `recommended_skills`, `recommended_agents`, `recommended_mcp_tools`
- `ward_recommendation`: `use_existing` with the exact ward name, or
  `create_new` with a reusable domain name and an archetype.
- `execution_strategy`: `simple` or `graph`, with an explanation.

The ward name must be a reusable domain category, never a task name.
GOOD: `financial-analysis`. BAD: `amd-q3-earnings`. The middleware
rejects a new ward when a listed ward already covers the domain.

## Phase 2: context assembly

The daemon composes the system prompt for the run:

```
# Role          (static, from config)
# Soul          (static)
# OS            (static, selected by operating system)
# Tools         (static)
# <Custom>*     (static, in order)
# --- dynamic sections below ---
# Context       memory records + knowledge graph, fused recall
# Task Analysis approach, ward pick, resources, step budget
# Skills        full bodies of the recommended skills
# Active Ward   the constitution of the ward (slice 3)
```

The `## Skills` section is the only way a skill enters the run. There
is no load_skill tool. The runtime provides the skills; the agent does
not fetch them.

The `## Context` section uses both chitta domains. The recall seam
fuses keyword, vector, and graph results. The simple run recalls
memory only. The goal run recalls memory and knowledge.

## Phase 3: the engine loop

1. The daemon calls `RunGoal` on the engine seam.
2. The eino adapter starts a bounded loop. The step budget from the
   intent contract bounds the loop. The default budget applies when no
   analysis ran.
3. In each step, the model sees the prompt, the history, and the tool
   list. It selects one action: a tool call, a delegation, or the
   final answer.
4. Each step, tool call, delegation, and error emits an event. The
   eventbus persists the event to the Timeline and streams it through
   SSE.
5. The loop stops when the model returns the final answer, or when the
   budget is used. A cancelled run stops at once.

### Delegation

The root agent delegates with the `delegate_agent` tool:

- The task must be complete and standalone. The child does not see the
  context of the parent.
- The agent must have a description. Agents without a description are
  not offered.
- `max_depth` in the agent file bounds the delegation depth. The
  seeded roster agents have depth 0: they delegate to nobody.
- The child runs with its own model override and its own tool list.
- The result of the child returns to the root as a delegation event.

Skills and agents stay different things. Skills enter the context as
provisions. Agents receive work through delegation. The system never
mixes them.

## Phase 4: ward hosting (planned, slice 3)

```
   intent contract: ward_recommendation
        |
        v
   +-----------------------------------------+
   |           root agent                    |
   |  ward_use("stock-analysis")             |
   |  ward_create("india-analysis",          |
   |              archetype="research")      |
   +-------------------+---------------------+
                       |
                       v
   +-----------------------------------------+
   |  THE WARD = a specialized mini bot      |
   |  ~/Documents/wards/stock-analysis/      |
   |  - AGENTS.md        the constitution    |
   |  - ward-conf.yaml   the layout law      |
   |  - stock-analysis.md  canonical page    |
   |  - log.md, pages/, sources/, data/...   |
   +-----------------------------------------+
```

The ward tools work like this:

- `ward_use(name)` makes the ward the host of the run. The run becomes
  sticky to that ward. All later steps of the session stay there.
- `ward_create(name, archetype)` makes a new ward from an archetype
  template. The templates live in the vault under `.templates/`. The
  daemon writes them once. The user owns them after the first write.
- When a run enters a ward, the daemon injects the `## Active Ward`
  section. This section holds the constitution of the ward (its
  `AGENTS.md`) and its layout template with a digest.
- The run then executes as the agent of that ward, under the
  constitution of that ward.
- The run gets ward-scoped file tools. These tools read and write
  inside the active ward only.
- Delegated children also receive the layout template. Children may
  read the ward and write through declared roles. A structural change
  to the ward goes back to the root as a bounded request.

The ward is user data. The daemon reads and lints the ward. The daemon
never changes user files, except the one-time template seeding.

## Diagram: the full pipeline

```
   CLIENT
     |  POST /api/goals
     v
 +---+--------------------------------------------+
 |                DAEMON (zbotd)                   |
 |                                                 |
 |  [1] INTENT ANALYSIS (pre-run)                  |
 |  +-------------------------------------------+  |
 |  | router ---- trivial? ---> bypass          |  |
 |  |   |                                       |  |
 |  |   v                                       |  |
 |  | intent agent (small model)                |  |
 |  |   prompt: intent-analysis.md              |  |
 |  |   slots: {{.Skills}} {{.Agents}}          |  |
 |  |           {{.Wards}}                      |  |
 |  |        |            |          |          |  |
 |  |        v            v          v          |  |
 |  |  resource index  resource   ward vault    |  |
 |  |  (chitta kind)   index      (Vault.Index) |  |
 |  |   skills/agents  mcp tools                |  |
 |  |   ----------------------------------     |  |
 |  |   typed contract: intents, path,          |  |
 |  |   complexity -> step budget, resources,   |  |
 |  |   ward pick (use_existing/create_new)     |  |
 |  +-------------------------------------------+  |
 |                     |  validation (drop + warn) |
 |                     v                           |
 |  [2] CONTEXT ASSEMBLY                           |
 |  +-------------------------------------------+  |
 |  | static: Role Soul OS Tools Custom         |  |
 |  | ## Context   chitta: memory + graph       |  |
 |  | ## Task Analysis                           |  |
 |  | ## Skills     provisioned bodies           |  |
 |  | ## Active Ward . . . . . . . (slice 3)    |  |
 |  +-------------------------------------------+  |
 |                     |                           |
 |                     v                           |
 |  [3] ENGINE LOOP (bounded, eino adapter)        |
 |  +-------------------------------------------+  |
 |  | model <- provider plane                   |  |
 |  | tools: builtin + chitta (+ MCP . . slice) |  |
 |  |                                           |  |
 |  |    +-> delegate_agent --> agent roster    |  |
 |  |    |        |            (max_depth,     |  |
 |  |    |        v             tool list)      |  |
 |  |    |    child run --> result event        |  |
 |  |    |                                       |  |
 |  |    +-> ward tools  . . . . (slice 3)      |  |
 |  |    |    ward_use / ward_create            |  |
 |  |    |        |                             |  |
 |  |    |        v                             |  |
 |  |    |    WARD VAULT (user data)            |  |
 |  |    |    ~/Documents/wards/<name>/         |  |
 |  |    |    constitution + layout + pages     |  |
 |  |    |                                       |  |
 |  |    +-> final answer                       |  |
 |  +-------------------------------------------+  |
 |                     |                           |
 |    every step --> eventbus --> Timeline --> SSE |
 |                                                 |
 |  storage: zbot.db (session, Timeline)           |
 |           chitta (memory + knowledge graph)     |
 |           ward vault (durable artifacts)        |
 +-------------------------------------------------+
```

Dashed items (`. . .`) are planned. Solid items are in the code or are
defined fully by change `add-intent-analysis`.

## Storage and memory

| Store | Holds | Who writes |
|---|---|---|
| `zbot.db` | Sessions, Timeline, compactions | The daemon |
| chitta | Memory records, knowledge graph, resource index | The daemon (distiller, registry watch) |
| Ward vault | Wards: constitution, layout, canonical pages, data | The user; agents through ward tools (slice 3) |
| Provider keyring | Credential values only | The daemon, in the platform keyring |

Slice 5 adds the bridge in both directions: ward pages become chitta
memory records, and distilled sessions can write into ward pages.

## What the goal run does not do

- It does not run without a budget. The step budget always bounds the
  loop.
- It does not execute a skill as code. A skill is context. Tools and
  agents do the work.
- It does not let a child change the structure of a ward. Only the
  root owns the workspace.
- It does not store state outside the device.

## Rules that apply

1. Only `internal/engine/eino/` imports eino. All other code uses the
   engine seam.
2. The intent analysis never calls tools and never browses.
3. Each recommended name is validated against the index before the run
   uses it. Invalid names are removed with a warning event.
4. Every step, delegation, tool call, and decision emits an event.
5. The ward vault stays outside `~/.zbot`. The daemon reads and lints
   it. The user owns it.

## Current and planned parts

| Part | Status |
|---|---|
| Engine seam, bounded loop, provider plane | In the code |
| Delegation (`delegate_agent`, depth grants, per-agent tools) | In the code |
| eventbus, Timeline, SSE streaming | In the code |
| Ward vault, linter, ward index, read-only API | In the code (slice 1) |
| Agent roster seeds (6 agents) and skill seeds (5 skills) | In the code |
| Intent middleware, typed contract, step budgets | Planned, `add-intent-analysis` |
| Composed system prompt, `## Skills` provisioning | Planned, `add-intent-analysis` |
| `## Context` recall of memory + knowledge in runs | Planned, `add-intent-analysis` |
| Ward tools, sticky ward, `## Active Ward`, ward file tools | Planned, `add-ward-hosting` |
| Ward pages to chitta bridge | Planned, `add-chitta-ward-bridge` |

## Related documents

- `simple-run-architecture.md` — the simple run (chat)
- `ward-research-exploration.md` — the ward concept and the slices
- `architecture-brief.md` — the founding record
- `../product/backlog.md` — the slice plan
