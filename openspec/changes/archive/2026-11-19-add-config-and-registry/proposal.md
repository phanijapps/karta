# Proposal

## Why

Day-1 step 2 of the brief: the daemon is configuration-driven — "zero hardcoded agents or tools" — so before any engine, server, or memory work, z-Bot needs the filesystem control plane: home resolution with overrides, first-run bootstrap, strict `config.yaml` loading, the five registry loaders (`prompts`, `agents`, `skills`, `hooks`, `mcps`), and `fsnotify` hot-reload. The design decisions were confirmed by the user: whole-root location override only, hard fail on invalid config, bootstrap creates structure but never content.

## What Changes

- Add **`backend/internal/config/`** — control-plane foundation:
  - Home resolution with precedence **`--home` flag > `ZBOT_HOME` env > `~/.zbot`** (flag wiring lands with the `zbotd` entrypoint; the resolution API accepts the override now). The override relocates the whole root: `config.yaml`, `config/**`, `data/`, `wards/`.
  - First-run bootstrap: missing home → create the tree (`config.yaml` with defaults, empty `config/{prompts,agents,skills,hooks,mcps}/`, `data/`, `wards/`). Default `config.yaml` written **only when absent**; existing files never overwritten; missing subdirs self-heal.
  - Strict `config.yaml` loading: strict YAML decode rejecting unknown keys; invalid file → **hard fail** with a precise error, never silent regeneration or default fallback.
  - Initial config schema: engine selection (default `eino`), server address, token budgets, wards root.
- Add **`backend/internal/registry/`** — the five loaders + hot-reload:
  - `prompts/`: `*.md` Go text/template fragments.
  - `agents/`: declarative agent specs as `*.yaml` or `*.md` with YAML frontmatter.
  - `skills/`: `<name>/SKILL.md` directories (Agent Skills frontmatter: `name`, `description`) with optional bundled scripts.
  - `hooks/`: `*.yaml` manifests (`event` from the six lifecycle names, optional `matcher` regex — must compile, `command`, `timeout_ms`).
  - `mcps/`: `*.yaml` MCP server manifests (stdio: command+args+env, or streamable HTTP: url).
  - Per-file error isolation: one invalid file never blocks loading the rest; every failure carries its path.
  - `fsnotify` hot-reload on the resolved home: debounced watch of `config.yaml` + `config/**`; reloads only affected entries; consumers notified of add/change/remove.
- **First external dependency**: `github.com/fsnotify/fsnotify` (domain purity unaffected — `config`/`registry` sit above `domain` in the graph).

Out of scope: the `zbotd` entrypoint and flag wiring, engine consumption of registry entries, hook execution (`internal/hooks/`), compaction, memory, channels, any daemon runtime.

## Capabilities

### New Capabilities

- `config-plane`: The control-plane foundation — home resolution with whole-root override, first-run bootstrap (structure, never content), strict `config.yaml` loading that hard-fails on invalid input, and self-healing of the directory tree.
- `config-registry`: The file-driven registry — five loader kinds over `~/.zbot/config/**` with per-file error isolation and debounced fsnotify hot-reload, owning nothing hardcoded.

### Modified Capabilities

None — `domain-model` seams are consumed, not changed; `agent-conventions` already states the rule this change implements.

## Impact

- **New files:** `backend/internal/config/*.go` (+tests), `backend/internal/registry/*.go` (+tests).
- **New dependency:** `fsnotify` in `backend/go.mod` (first external dep; `go mod tidy` updates; zero-dep invariant from the workspace change ends by design).
- **Testing:** the precedence tests prove the `ZBOT_HOME` override end-to-end; the rest of the suite runs against `t.TempDir()`-isolated homes built directly, so no test ever touches the real `~/.zbot`.
- **Downstream:** the engine adapter (skills, sub-agents, MCP), hook executor, and server all consume this registry; hot-reload means daemon restarts are not required for control-plane edits.
