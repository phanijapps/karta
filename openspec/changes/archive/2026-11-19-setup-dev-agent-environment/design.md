# Design

## Context

The repository contains only `openspec/` (fresh init). All z-Bot development will be agent-driven against the Architecture & Scaffolding Brief (currently at `~/Downloads/z-Bot — Architecture & Scaffolding Brief.md`). This change creates the dev-agent layer **before** any implementation, so later Day-1 changes are developed under the rules rather than retrofitting them. See proposal.md — Why.

Environment facts that shape placement (verified on this machine):
- pi discovers skills from project `.agents/skills/` (Agent Skills spec, scanned from the working directory through ancestors, stopping at the repo root); the same path convention is portable to other harnesses.
- pi-subagents discovers project agent definitions from `.pi/agents/**/*.md` (markdown: YAML frontmatter + system prompt). Project agents require the session's Pi project trust.
- pi reads `AGENTS.md` files as project memory automatically.

## Goals / Non-Goals

**Goals:**
- Constitution, skills, sub-agents, and product context all live workspace-local in this repo and are versioned with it.
- Every artifact is machine-discoverable (correct paths/frontmatter), not just human-readable documentation.
- Content is grounded in the brief and stays traceable to it.

**Non-Goals:**
- No Go/Svelte implementation, no runtime `~/.zbot/` control plane, no runtime skills/agents for the daemon itself.
- No exhaustive per-domain skill set (wards, memory, registry, channels skills arrive with their first implementation changes).
- No CI or lint automation for markdown content in this change.

## Decisions

### D1: Skills at `.agents/skills/<name>/SKILL.md` (Agent Skills spec)

Workspace-local `.agents/skills/` gives automatic discovery by pi and portability (Agent Skills spec; Claude Code-compatible layout) without touching user-global `~/.agents` or `~/.pi`. Alternatives: `.pi/agent/skills/` (pi-only, less portable) or user-global install (violates workspace-local requirement, not versioned with the repo). Each skill gets a routing description that states what it covers and when to apply it; body files stay single `SKILL.md` (no scripts/references yet — nothing to script against in an empty repo).

### D2: Sub-agents at `.pi/agents/*.md` with `zbot-` prefix

`zbot-scout`, `zbot-backend`, `zbot-frontend`, `zbot-reviewer`. The prefix avoids colliding with builtin agents (`scout`, `reviewer`) — we want additive specialists, not overrides. All four set `inheritProjectContext: true` so each child session loads the AGENTS.md constitution; `inheritSkills: true` for the workers so domain skills are available inside children. `zbot-reviewer` is read-only (`acceptanceRole: read-only`, tools limited to read/grep/find/ls) with a boundary checklist derived from the constitution: engine isolation, FSD import direction, dependency graph, contract-first API, eventbus emission. Alternative: reuse generic builtin `reviewer` — rejected because the boundary rules are project-specific and belong in a versioned project agent.

### D3: Three-layer constitution matching brief §7

Root `AGENTS.md` (four global rules) + `backend/AGENTS.md` + `frontend/AGENTS.md`, per the brief's blueprints. Scoped files load only when work touches their subtree, keeping each session's constitution small and precise. A `desktop/AGENTS.md` is deliberately deferred until `desktop/` code first lands. The files state rules as constraints with concrete package-path examples from the brief's target structure, framed as the target architecture (the repo is empty today).

### D4: Three initial skills, chosen by violation-cost

`eino-engine-adapter`, `contract-first-api`, `svelte5-fsd-frontend` — the domains where a wrong turn is costliest to reverse (engine lock-in, contract drift, frontend architecture). Compaction and hooks intentionally have no dedicated skill (user decision): those rules live in `backend/AGENTS.md` and the `zbot-backend` agent prompt; add dedicated skills later only if needed. Each skill's content cites the constitution as authority and includes the brief's concrete procedures (e.g. middleware adapter flow, runes/FSD rules, `WAILS_VITE_PORT` config) rather than restating the whole brief.

### D5: Product context + preserved founding brief

`docs/architecture/architecture-brief.md` = verbatim copy of the founding brief (historical record). `docs/product/product-context.md` = distilled, living product truth (vision, users, core concepts, non-goals) written from the brief, with the precedence rule: on later disagreement, product-context wins for product matters. Alternative: treat the brief itself as product context — rejected: it is architecture-heavy and will age; the spec requires a maintained, decision-grading source.

### D6: Populate `openspec/config.yaml` `context:`

A short context block (stack summary + pointers to `AGENTS.md`, `docs/architecture/architecture-brief.md`, `docs/product/product-context.md`) so every future OpenSpec planning run inherits the same constraints without re-reading the brief. Kept well under the size limit; pointers, not copies.

## Risks / Trade-offs

- [Skills describe a codebase that doesn't exist yet] → They are framed as target-architecture guidance citing the brief; each domain's first implementation change updates its skill from target to as-built.
- [Rule drift between brief, constitution, skills, and agents] → Spec requires same-change updates; `zbot-reviewer`'s checklist and the constitution-as-authority rule give one enforcement point per boundary.
- [Project trust: if the pi session declines project trust, project skills/agents are ignored] → Apply ends with a discovery verification step (fresh session listing skills/agents); noted for the operator.
- [Markdown rules are advisory, not mechanically enforced] → Accepted for now; mechanical checks (import-boundary lint) arrive with the Go workspace, where they can run as tests.
- [Naming collisions with future harness builtins] → `zbot-` prefix on all four agents; skill names are domain-specific and namespaced by the repo anyway.

## Migration Plan

New files only; no migration. Rollback = delete the created files and restore `openspec/config.yaml`'s commented-out context block.

## Open Questions

- Add `desktop/AGENTS.md` when the Wails shell first lands, or fold desktop rules into the root constitution? Safe to decide at that change; does not affect this one.
