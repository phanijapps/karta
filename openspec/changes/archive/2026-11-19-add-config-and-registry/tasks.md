# Tasks

## 1. Config package (backend/internal/config/)

- [x] 1.1 Add deps (`gopkg.in/yaml.v3`, `github.com/fsnotify/fsnotify`) via `go get` + `go mod tidy`; verify `go build ./...` and that `internal/domain` purity test still passes (deps live above domain only)
- [x] 1.2 Implement home resolution (`ResolveHome(flagOverride)`: flag > `ZBOT_HOME` > `~/.zbot`) and the `Paths` struct deriving `config.yaml`, the five config dirs, `data/`, `wards/`; verify table-driven tests cover default/env/flag precedence (using temp dirs via env override)
- [x] 1.3 Implement bootstrap (create-only default `config.yaml` via O_EXCL marshalled from typed defaults, MkdirAll tree, never overwrite, idempotent) and self-heal (recreate missing subdirs, touch nothing); verify tests: first-run tree, existing config bytes unchanged, double-bootstrap no-op, partial-home repair
- [x] 1.4 Implement strict `Config` loading (yaml.v3 `KnownFields(true)`, path-wrapped errors, no default fallback) with v1 schema (engine default `eino`, server address, compaction budgets, wards root resolved against home); verify tests: valid load, unknown key rejected with key name, invalid YAML rejected with position, absent-after-bootstrap rejected
- [x] 1.5 Add `backend/internal/config/AGENTS.md` (responsibility, boundaries, entrypoints — DoD #4); verify it names resolution precedence and the never-overwrite rule

## 2. Registry loaders (backend/internal/registry/)

- [x] 2.1 Implement the snapshot/registry core: immutable snapshot maps per kind, `atomic.Pointer` swap, `LoadError{Path, Kind, Err}` collection, skip of hidden/temp files; verify a test loading an empty plane yields empty registry + zero errors and one bad file among many yields one path-named error
- [x] 2.2 Implement prompts loader (`*.md` Go text/template, stem = name); verify tests: valid template addressable by name, broken template reported with path while others load
- [x] 2.3 Implement agents loader (`*.yaml` + `*.md` YAML frontmatter; name, system prompt literal or prompt reference, model overrides, tool allowlist; duplicate names reported with deterministic winner); verify tests: YAML form, frontmatter form equivalently loaded, duplicate-name conflict reported
- [x] 2.4 Implement skills loader (`<name>/SKILL.md` with Agent Skills frontmatter `name`+`description`, bundled files tracked); verify tests: valid skill exposed with directory, missing-frontmatter skill reported, other skills unaffected
- [x] 2.5 Implement hooks loader (`*.yaml`: event enum of six lifecycle names, optional compiling matcher regex, required command, `timeout_ms`); verify tests: valid manifest bound to event, unknown event rejected, non-compiling matcher rejected — each with path
- [x] 2.6 Implement MCP loader (`*.yaml`: stdio command+args+env OR streamable-http url; neither/both rejected); verify tests: stdio variant loads, http variant loads, ambiguous rejected
- [x] 2.7 Add `backend/internal/registry/AGENTS.md` (five kinds, snapshot semantics, per-file isolation, nothing hardcoded); verify it matches this design

## 3. Hot-reload

- [x] 3.1 Implement the watcher: recursive watch via walk-added dirs (new dirs watched + scanned), 150ms path-keyed debounce, classify by re-reading disk after the window, atomic snapshot swap, subscriber callbacks with add/change/remove, context/Close lifecycle ending the goroutine; verify a leak test (goroutine count returns to baseline after Close, in-test baseline)
- [x] 3.2 Write behavioral tests with real fs events (create/edit/delete across at least prompts and hooks kinds, directory creation mid-run, editor-style burst coalescing to exactly one reload); verify tests poll with timeouts and pass under `-race`

## 4. Wiring and verification

- [x] 4.1 Run the constitution Verification suite: `task codegen` (contract unchanged, still valid), `task lint`, `task lint:backend` (golangci-lint), `task test` (race on), `task build`, `task verify` end-to-end; verify all green after the final edit
- [x] 4.2 Constitution boundary check: no eino imports; domain still imports nothing beyond stdlib; config/registry import only domain-ward per the graph; verify with the boundary greps
- [x] 4.3 Run `openspec validate add-config-and-registry --strict` and verify it passes

## Workflow follow-up

- Review (zbot-reviewer), then archive the change (syncs 2 new capability specs into `openspec/specs/`).
