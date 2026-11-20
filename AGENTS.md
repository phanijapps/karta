# Karta — Repository Constitution

This file is the root constitution for every coding agent working in this
repository. Scoped constitutions (`backend/AGENTS.md`, `frontend/AGENTS.md`)
add rules for their subtrees. Product truth lives in
[`docs/product/product-context.md`](docs/product/product-context.md); the
founding record in [`docs/architecture/architecture-brief.md`](docs/architecture/architecture-brief.md).

## Product Identity

The app and product are named **Karta**. Use Karta in user-facing UI and current
documentation. Existing `zbot` code identifiers, daemon/CLI names,
`zbot-reviewer`, and `~/.zbot/` storage paths remain unchanged for compatibility;
the rebrand does not authorize an API or data migration.

## Global Rules

1. **Local-first storage.** All persistent state lives under `~/.zbot/`: the
   SQLite database at `~/.zbot/data/zbot.db` (sessions, Timeline Tree,
   sqlite-vec, FTS5, knowledge graph) and the configuration tree
   `~/.zbot/config/{prompts,agents,skills,hooks,mcps}/**`. No cloud storage,
   no remote state, no data leaving the device.
2. **Contract-first API.** `contracts/openapi.yaml` is the single source of
   truth for the daemon HTTP/SSE API. Endpoint changes update the contract
   first, then regenerate clients (`task codegen`), then implement handlers.
   No handler-first changes, no hand-rolled client bindings.
3. **Channel-agnostic core.** No package under `backend/internal/` may import
   UI, CLI, Wails, or messaging-plugin code. Clients (`frontend/`,
   `desktop/`, `cmd/zbot`) consume the daemon over the contract; core packages
   never know them.
4. **Eventbus emission.** Every agent step, delegation, tool call, hook
   decision, and compaction event emits to `internal/eventbus` so it persists
   to the Timeline Tree and streams to all clients via SSE. Silent work is a
   bug.

## Repository Map (target structure)

```
contracts/openapi.yaml   # API source of truth
backend/                 # Go daemon (zbotd) + Bubbletea CLI (zbot)
frontend/                # Svelte 5 SPA (pure, zero SSR)
desktop/                 # Wails v3 shell (thin)
docs/product/            # product truth
docs/architecture/       # founding brief & architecture records
.agents/skills/          # workspace-local development skills
.pi/agents/              # workspace-local sub-agent roster
```

## Working Rules

- Planning changes: use OpenSpec (`openspec/`). Specs live in
  `openspec/specs/`, in-flight changes in `openspec/changes/`.
- This constitution is the authoritative statement of the brief's rules. A
  change that alters any rule encoded here updates the corresponding AGENTS.md
  file in the same change.
- No god classes. Keep it simple; Keep it modular; Keep it explainable.
- Don't boil the ocean - Simple comments so that the next agent can understand the crap you spat out.

### Backend Rules
- Add a simple AGENTS.md explaining in each package


## Verification

Run the checks relevant to what you touched. Never claim a task is complete based on reading code alone—run the commands and inspect the output.

- **Contracts (if `contracts/openapi.yaml` or handlers changed):**
  - `task codegen` — regenerate Go server types and TS client; verify zero uncommitted drift.
- **Backend (`backend/`):**
  - `task test:backend` (or `go test -race ./...`) — all unit/integration tests pass using in-memory SQLite and fake LLM/engine stubs (zero live API calls).
  - `task lint:backend` (`golangci-lint run`) — zero lint errors.
  - **Boundary check:** confirm `github.com/cloudwego/eino` is not imported outside `backend/internal/engine/eino/`, and no `backend/internal/` package imports `frontend/`, `desktop/`, or `cmd/`.
- **Frontend (`frontend/`):**
  - `task check:frontend` (`pnpm check` / `svelte-check` + `pnpm build`) — zero TypeScript, Svelte 5 rune, or static build errors.
- **Desktop (`desktop/`, if shell or build config changed):**
  - `task build:desktop` — Wails v3 compiles cleanly with embedded `frontend/dist`.
- **Full repo (before opening a PR or closing an OpenSpec change):**
  - `task verify` — runs codegen check, lint, tests, and build end-to-end.

- **Documents Updated:**
  - Documents in `docs/` need to be updated as implemenation changes. (product, architecture or guidelines)

## Definition of Done

A task is **not** done until every applicable box is true:

1. **Spec & Constitution Aligned:** Implementation matches the active `openspec/changes/` spec. If any architectural rule changed, the corresponding `AGENTS.md` was updated in the same change.
2. **Contract Synced:** Any API/SSE change started in `contracts/openapi.yaml`, regenerated via `task codegen`, and has matching handler + client usage.
3. **Observable:** New agent steps, tool calls, delegations, hook decisions, or compaction runs emit structured events to `internal/eventbus` and have test coverage proving emission.
4. **Package Docs Updated:** Every new Go package under `backend/internal/` has a short, plain-English `AGENTS.md` explaining its responsibility, boundaries, and key entrypoints.
5. **Verified Locally:** All relevant Verification commands ran and passed **after** the final edit—no skipped tests, no `TODO` stubs left in critical paths, no live network calls in unit tests.
6. **Clean Diff:** No unrelated refactors, debug logs, or leftover generated junk. Ready for the next agent to read cold.
7. **Reviewed:** The `zbot-reviewer` subagent ran read-only over the change; every P0/P1 finding is fixed; `task verify` is green after the final edit. P2s are fixed or the reason is recorded in the change summary.
