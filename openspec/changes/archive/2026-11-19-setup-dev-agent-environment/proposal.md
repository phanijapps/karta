# Proposal

## Why

The z-Bot repository (Go daemon + Svelte 5 SPA + Wails v3 shell) is about to be built from scratch against the Architecture & Scaffolding Brief, and nearly all development will be done by AI coding agents. Before the first line of backend or frontend code lands, those agents need the project's constitution, specialized know-how, delegation roster, and product grounding in place — otherwise every Day-1 scaffolding change re-derives the rules from scratch and drifts from the brief (engine isolation, FSD imports, contract-first API, local-first storage).

## What Changes

- Add **root `AGENTS.md`** — the repository constitution from the brief §7: local-first storage, contract-first API development, channel-agnostic core, eventbus emission requirements.
- Add **`backend/AGENTS.md`** — engine isolation rule (only `internal/engine/eino/` imports `github.com/cloudwego/eino`), compaction placement rule, downward dependency graph, table-driven testing rules.
- Add **`frontend/AGENTS.md`** — Svelte 5 runes-only rule (no legacy `$:`/`export let`), FSD import rules (`app` → `features` → `entities` → `shared`), generated-API-client-only rule.
- Add **three development skills** under this workspace's `.agents/skills/` (Agent Skills spec, discovered by pi and compatible harnesses from the repo): `eino-engine-adapter`, `contract-first-api`, `svelte5-fsd-frontend`. Skills live workspace-local only — nothing is installed into user-global `~/.agents` or `~/.pi`.
- Add **four sub-agent definitions** under this workspace's `.pi/agents/`: `zbot-scout` (repo structure recon), `zbot-backend` (Go backend worker), `zbot-frontend` (Svelte 5 worker), `zbot-reviewer` (constitution/boundary enforcement). Apply must materialize both directories in the workspace root so the harness discovers them from this repo.
- Add **`docs/product/product-context.md`** — the product context document (vision, users, core concepts, non-goals) that grounds every agent's product-level decisions.
- Preserve the founding brief in-repo as **`docs/architecture/architecture-brief.md`** and populate `openspec/config.yaml` `context:` so future planning runs inherit the same constraints.
- Add a minimal root `.gitignore` (scratch `progress.md`, build outputs, local env).

Out of scope: any Go/Svelte implementation code, the runtime control plane (`~/.zbot/`), runtime skills/agents for the z-Bot daemon itself, and OpenSpec specs for product capabilities (those arrive with their own changes).

## Capabilities

### New Capabilities

- `agent-conventions`: The AGENTS.md constitution layer — root and scoped architecture rule files that every coding agent working in the repo loads, covering local-first storage, contract-first API, channel-agnostic core, engine isolation, compaction placement, dependency graph, Svelte 5/FSD rules, and testing discipline.
- `dev-agent-tooling`: The on-demand development assets — skills under `.agents/skills/` (frontmatter-valid SKILL.md packages for specialized z-Bot workflows) and sub-agent definitions under `.pi/agents/` (the delegation roster for backend, frontend, scout, and review work).
- `product-context`: The product documentation contract — `docs/product/product-context.md` as the single maintained source of product truth (vision, users, concepts, non-goals) that agents consult before product-level decisions, kept coherent with the preserved architecture brief.

### Modified Capabilities

None — this is the first change in the repository.

## Impact

- **New files only** (repo is empty apart from `openspec/`): `AGENTS.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`, `.agents/skills/**` (3 skills), `.pi/agents/**` (4 agents), `docs/product/product-context.md`, `docs/architecture/architecture-brief.md`, `.gitignore`, edit to `openspec/config.yaml` (uncomment + fill `context:`).
- **No code, API, or dependency impact** — this change affects how future changes are developed, not runtime behavior.
- **Agent harness integration**: skills become auto-discoverable by pi at session start; sub-agents become launchable by name via pi-subagents; `AGENTS.md` is read as project memory by coding agents.
- **Downstream**: every subsequent Day-1 scaffolding change (workspace, registry, compaction, engine adapter, CLI, memory, frontend) is expected to be developed using these assets and reviewed against them.
