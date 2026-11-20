# Tasks

## 1. Docs foundation

- [x] 1.1 Copy the founding brief verbatim from `~/Downloads/z-Bot — Architecture & Scaffolding Brief.md` to `docs/architecture/architecture-brief.md`; verify with `diff` that the copy is byte-identical
- [x] 1.2 Author `docs/product/product-context.md` from the brief (vision, target users, core concepts: daemon/client split, engine seam, compaction, Timeline Tree, Code Wards, control plane, local-first memory; explicit non-goals; precedence note over the brief); verify every required section is present and no statement contradicts `architecture-brief.md`

## 2. Constitution (AGENTS.md layer)

- [x] 2.1 Write root `AGENTS.md` per brief §7 (local-first storage, contract-first API with `contracts/openapi.yaml`, channel-agnostic core, eventbus emission; plus repo map pointers to docs and openspec); verify all four global rules are stated with concrete package-path examples
- [x] 2.2 Write `backend/AGENTS.md` (engine isolation: only `internal/engine/eino/` imports eino; compaction placement in `internal/compaction/`; downward dependency graph; table-driven tests with in-memory SQLite and fake engines, never live APIs); verify each rule matches brief §7 and `backend/` exists with only this file
- [x] 2.3 Write `frontend/AGENTS.md` (Svelte 5 runes only, no `$:`/`export let`; FSD `app` → `features` → `entities` → `shared`, no cross-feature imports; generated API client only; Wails platform isolation behind `src/shared/platform/`); verify each rule matches brief §7 and `frontend/` exists with only this file

## 3. Development skills (`.agents/skills/`)

- [x] 3.1 Create `.agents/skills/eino-engine-adapter/SKILL.md` (isolation boundary, ADK/DeepAgents adapter patterns, middleware adapters, engine-swap procedure); verify frontmatter has `name` + routing `description` and body cites the constitution as authority
- [x] 3.2 Create `.agents/skills/contract-first-api/SKILL.md` (`contracts/openapi.yaml` → codegen → chi handlers + SSE streaming flow); verify frontmatter is valid and the flow forbids handler-first changes
- [x] 3.3 Create `.agents/skills/svelte5-fsd-frontend/SKILL.md` (runes usage, FSD import rules, generated API client, `WAILS_VITE_PORT` 9245 + `strictPort`, platform adapter isolation); verify every rule aligns with `frontend/AGENTS.md` and the brief §6

## 4. Sub-agent roster (`.pi/agents/`)

- [x] 4.1 Create `.pi/agents/zbot-scout.md` (repo structure recon against the brief's target monorepo map, where-new-code-belongs routing); verify frontmatter (`name`, `description`, read-only tools, `inheritProjectContext: true`) parses as YAML and the prompt references the repo map and product context
- [x] 4.2 Create `.pi/agents/zbot-backend.md` (Go backend worker: domain-first design, engine isolation, dependency graph, table-driven testing discipline, escalate unapproved decisions); verify frontmatter grants write tools and the prompt encodes the backend constitution rules
- [x] 4.3 Create `.pi/agents/zbot-frontend.md` (Svelte 5 worker: runes-only, FSD boundaries, generated client, SSE store patterns); verify frontmatter grants write tools and the prompt encodes the frontend constitution rules
- [x] 4.4 Create `.pi/agents/zbot-reviewer.md` (read-only boundary reviewer: engine isolation, FSD import direction, dependency graph, contract-first API, eventbus emission); verify `acceptanceRole: read-only` with read-only tools and the checklist covers all five boundaries

## 5. Wiring and integration checks

- [x] 5.1 Populate `openspec/config.yaml` `context:` with a short block pointing to `AGENTS.md`, `docs/architecture/architecture-brief.md`, and `docs/product/product-context.md` plus the locked-stack summary; verify `openspec context --json` still resolves the root and the YAML parses
- [x] 5.2 Add root `.gitignore` (scratch `progress.md`, `frontend/dist/`, `node_modules/`, `desktop/build/`, `.env`, local databases); verify `git status` stays clean when a `progress.md` scratch file is created and removed
- [x] 5.3 Verify workspace-local discovery: all three skills exist only under the repo's `.agents/skills/` (nothing in `~/.agents/skills/` or `~/.pi/agent/skills/`) and all four agents exist only under `.pi/agents/` (nothing in `~/.pi/agent/agents/`); confirm a fresh agent session in the repo lists the skills by name
- [x] 5.4 Run `openspec validate setup-dev-agent-environment --strict` and verify it passes
