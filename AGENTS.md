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
   compactions — conversations only) and the chitta database at
   `~/.zbot/data/chitta/` (ONE LadybugDB holding both chitta domains:
   memory records and the knowledge graph, with their cross-domain
   links), plus the configuration tree `~/.zbot/{agents,skills,hooks}/**`
   (skills and hooks are TOP-LEVEL user content, relocated from
   config/ — migrateLegacySkillsHooks moves classic installs once) and
   `~/.zbot/config/{prompts,mcps,providers}/**`. Two scoped
   exceptions: (a) provider credential VALUES for daemon-managed providers
   live in the platform keyring (Linux Secret Service, macOS Keychain,
   Windows Credential Manager), namespaced per home root — never under
   `~/.zbot/`, never in files, responses, or logs, and never a plaintext
   fallback; (b) the **ward vault** — user-chosen local data — lives
   OUTSIDE `~/.zbot` (default OS-aware `~/Documents/wards`, overridable
   via `wards.vault_root` in config.yaml; absolute paths only, and the
   resolver REJECTS roots inside `~/.zbot` — wards are documented
   knowledge the user owns, not daemon state). The daemon only reads
   and lints the vault; bootstrap creates it and seeds the archetype
   templates plus a `.gitignore` starter write-once, never overwriting
   user files. No cloud storage, no remote state, no other data leaving
   the device.
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
  - `task lint:backend` (`golangci-lint run`, configured by `backend/.golangci.yml` — errcheck blank-discard, revive exported, errorlint) — zero lint errors.
  - `task gates:backend` (`scripts/backend-gates.sh`) — mechanical engineering gates: 500-LOC file budget with named grandfathers, banned generic names, `ctx-ok:` context markers, test floor, AGENTS.md coverage, and the constitution boundaries (eino isolation, channel-agnostic core) — these replaced the hand-run greps.
  - `task budget:backend` — suite runtime budget (machine-qualified, see go-rules 4.3).
  - The backend engineering rules these enforce live in [`docs/development/go-rules.md`](docs/development/go-rules.md) — every rule states its rationale and enforcement; a rule without enforcement does not belong there.
- **Frontend (`frontend/`):**
  - `task check:frontend` (`pnpm check` / `svelte-check` + `pnpm build`) — zero TypeScript, Svelte 5 rune, or static build errors.
- **Desktop (`desktop/`, if shell or build config changed):**
  - The Wails v3 shell has not landed yet; when it does, its build task
    (compiling with embedded `frontend/dist`) joins this list — added here
    only then, not before (reviewer finding: this bullet previously named a
    `task build:desktop` that does not exist).
- **Full repo (before opening a PR or closing an OpenSpec change):**
  - `task verify` — gates, codegen (+ contract-drift regenerate-and-compare), lint, tests (incl. real-LadybugDB suite), runtime budget, and build end-to-end. A contract change runs with `CONTRACT_CHANGE=1` declared and the diff recorded in its change.

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
