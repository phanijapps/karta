# Session Handoff — Karta Ward Research

> Written at the end of a long implementation session (2026-11-23).
> This file captures everything a fresh session needs to continue.
> Delete after the next session reads it.

## Where everything stands

### Completed & archived (specs synced to main)
1. **add-ward-vault** (slice 1) — vault, linter, read-only API, Wards UI surface
2. **seed-operator-roster** — 6 agent seeds + 5 skill seeds + persona seeds

### Completed, committed, WAITING FOR HUMAN REVIEW (not archived)
3. **add-intent-analysis** (slice 2) — 14/14 tasks, all gates green, verify exit 0
   - Intent middleware, resource index, system-prompt composition, skill provisioning
   - Findings ledger: `openspec/changes/add-intent-analysis/findings.md` (I1-I26 + final)
4. **relocate-skills-hooks** — 6/6 tasks, gates green, verify exit 0
   - Skills/hooks to top-level ~/.zbot/{skills,hooks}, migration, packaging sink keys
   - Findings ledger: `openspec/changes/relocate-skills-hooks/findings.md` (R1-R6 + final)

### In-flight (the CURRENT work)
5. **add-ward-hosting** (slice 3) — ~60% done
   - DONE: ward tools (ward_use/ward_create/file tools), engine wiring, sticky
     hosts, delegation guard, intent→ward-tool instruction, chat ward plane
     (ChatToolSet + research-protocol.md prompt), progress-aware goal runs
     (ProgressTracker with nudges/auto-extension/stuck-kill — the agentzero shape)
   - VERIFIED LIVE: a goal run entered stock-analysis ward, wrote spec.md +
     plan.md + data/yf_xcheck.py, delegated to 3 agents, wrote the canonical
     page brief — all in ~/Documents/wards/stock-analysis/
   - REMAINING: ward_entered/ward_created Timeline event kinds, ## Active Ward
     prompt section for runs starting in a sticky ward, delegate-description
     guard paragraph, phase gates + final review
   - Tasks: `openspec/changes/add-ward-hosting/tasks.md`

6. **add-chat-modes** — RESHAPED around the session model, ready to build
   - The operator rejected the mode-toggle model. The insight: **wards ARE
     sessions**. Each ward bot has its own persistent conversation (like
     quick chat); research sessions start as shells and enter wards.
   - Mock UI: `docs/architecture/mock-session-model.html` (open in browser —
     this is the design reference)
   - Full artifacts: `openspec/changes/add-chat-modes/{proposal,design,tasks}.md`
   - What survives from earlier toggle work: ChatMode→SessionType, sessions
     column (mode→type+ward), SendMode seam, Continue endpoint, Task Analysis
     rendering, ward tools
   - What's new: session CRUD, session-scoped chat-send, ward binding,
     sidebar redesign (session navigator), context panel (ward+skills+agents)

## The operator's design decisions (recorded across the session)

1. **Wards are mini-bots with their own constitution** — "Stock analysis",
   "India analysis". A ward IS a specialized agent. When a run enters a ward,
   it runs as that ward's agent, under its AGENTS.md.
2. **The chat IS the research surface** — no separate goal-run surface for
   research. The conversation routes to wards via the model's own judgment
   (the research-protocol.md prompt), not middleware.
3. **The session model** (the final architecture):
   - Quick Chat = ONE persistent lean session (always there)
   - Research = MULTIPLE sessions, each its own thread
   - Ward bots = persistent sessions bound to a ward (sticky)
   - [+ New Research] creates a shell session; intent analysis moves it to a ward
   - NO mode toggle — the session type IS the mode
4. **Skills are runtime-provisioned** — injected into ## Skills section, no
   load_skill tool. The runtime provides; the agent doesn't fetch.
5. **Simple chat skips intent analysis** — always lean, memory-only Context.
6. **Goal runs use memory AND knowledge** — both chitta domains (memory +
   knowledge graph), fused recall split by lane.
7. **Progress-aware budgets** (the agentzero shape) — nudges instead of hard
   stops, auto-extension for productive agents, stuck-only kill. The engine
   never stops a productive run.
8. **Persona in both modes** — Role+Soul+OS+Tools never change with mode.
9. **~/zbot/{skills,hooks}** — top-level user content (the operator hand-moved
   skills there; the loader follows).

## Key files to read in a fresh session

```
AGENTS.md                                    # the constitution
docs/architecture/ward-research-exploration.md  # the ward concept + slices
docs/architecture/mock-session-model.html    # the session model UI reference
docs/architecture/simple-run-architecture.md # chat architecture (STE)
docs/architecture/goal-run-architecture.md   # goal run architecture (STE)
docs/product/backlog.md                      # the slice plan
openspec/changes/add-chat-modes/             # THE NEXT CHANGE TO BUILD
openspec/changes/add-ward-hosting/           # slice 3 (finish first?)
openspec/changes/add-intent-analysis/        # waiting for review
openspec/changes/relocate-skills-hooks/      # waiting for review
backend/internal/wardtools/                  # the ward tool plane
backend/internal/progress/                   # the progress tracker
backend/internal/intent/                     # the intent middleware
backend/internal/sysprompt/                  # the composed system prompt
backend/internal/resourceindex/              # the semantic resource index
```

## Running services

- **zbotd** on :8787 (air-managed hot reload: `cd backend && air -c .air.toml`)
- **Vite** on :9245 (proxies /api to :8787)
- Dev stack: `task dev:all` (both together)

## Known open issues

1. **Resource-index revise primary-key collision** on real LadybugDB (non-fatal,
   logs on boot). Needs investigation — the fake driver masked it.
2. **Goal POST latency = analysis latency** (~24-38s). Needs a decision:
   bounded timeout or async analysis.
3. **Pre-existing parallel-test flakiness** — watcher/guard tests fail when
   packages run in parallel (all 27 pass with `-p 1`). Not caused by our changes.
4. **The budget gate was bumped** 25→28s (mode tests added ~2s).

## The operator's prompt set (at ~/Documents/zbot/config/)

- `intent-analysis-prompt.md` — the intent prompt (adapted into our seed)
- `agent-prompts/first-turn-protocol.md` — the fast-path/assessment shape
- `agent-prompts/tooling-skills.md` — tool discipline
- `SOUL.md`, `OS.md` — persona reference (NOT copied — our seeds are neutral)

## What to do first in the fresh session

1. Read `openspec/changes/add-chat-modes/design.md` + the mock UI
2. Read this file
3. Decide: finish add-ward-hosting first, or build add-chat-modes (they're
   independent — ward-hosting can land separately)
4. Apply the change with the same playbook: phase gates (gpt-6.1-terra +
   gpt-6.1-sol per task group), final zbot-reviewer, task verify
5. The operator reviews + archives the completed changes
