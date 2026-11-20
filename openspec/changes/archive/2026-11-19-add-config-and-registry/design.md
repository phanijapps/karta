# Design

## Context

Day-1 step 2 per the brief: control plane + registry with hot-reload, on top of the domain seams and eventbus from `scaffold-workspace-contracts`. User-confirmed decisions (recorded in the proposal): whole-root override only, hard fail on invalid `config.yaml`, bootstrap creates structure never content. The daemon entrypoint that passes the `--home` flag does not exist yet — this change exposes the resolution API it will call.

## Goals / Non-Goals

**Goals:**
- Every test runs against an isolated temp home via the override mechanism — proving the override by using it.
- Registry reads never observe partial state (atomic snapshots per load/reload).
- Hot-reload survives editor save bursts and directory creation.

**Non-Goals:**
- No `zbotd` entrypoint/flag wiring, no engine/hook execution, no MCP connections, no timeline emission for registry changes.
- No config schema migration/versioning machinery beyond strict decode of v1 fields.

## Decisions

### D1: Whole-root resolution, three inputs

`config.ResolveHome(flagOverride string)`: flag wins, then `ZBOT_HOME`, then `~/.zbot` (from `os.UserHomeDir`). All paths derive from the resolved home — one `Paths` struct (`ConfigFile`, `ConfigDir(kind)`, `DataDir`, `WardsDir`) so no call site re-derives paths. Partial overrides rejected by design (user decision).

### D2: Bootstrap is create-only

Default `config.yaml` is written with `O_CREATE|O_EXCL` — an atomic "only if absent" — with defaults marshalled from the typed `Config` so docs and code share one source. Directories are `MkdirAll(0o755)`. Nothing else is written; nothing is ever overwritten; idempotent by construction.

### D3: Strict load, hard fail

`yaml.Decoder` with `KnownFields(true)`; every failure wrapped with the file path; no default fallback on error (user decision). Absent file after bootstrap is an error (bootstrap guarantees it, so absence means external deletion). `Config` v1 fields: `engine` (default `eino`), `server.address`, `compaction` token budgets, `wards.root` (resolved against home).

### D4: Two external deps land: `yaml.v3` + `fsnotify`

YAML parsing and watching cannot be stdlib-only; both are stable, widely used, and confined to `config`/`registry` (above `domain` in the graph — purity test unaffected). Ends the zero-dep invariant from the workspace change by design, recorded here.

### D5: Registry = atomic snapshots + typed per-file errors

Each load builds an immutable snapshot (maps by name per kind); the registry holds `atomic.Pointer[Snapshot]` — readers always see a complete state, reloads swap wholesale. Failures are `LoadError{Path, Kind, Err}` slices returned alongside the snapshot; one bad file never blocks others. Frontmatter parsing for agents: split `---` delimiters, decode frontmatter strictly, body is the system prompt.

### D6: Hot-reload = one watcher, walk-added dirs, path-keyed debounce

fsnotify has no recursive watches: on start, walk `config/**` adding each directory; on a directory-create event, add it and scan it. Events funnel into a 150ms path-keyed debounce (one timer per pending path, reset on new events for that path — coalescing editor bursts); after the window, classify by path: which kind, which name (stem/dir), reload or unregister, then notify subscribers. Subscribers are callbacks (`OnChange(func(Change))`); registry changes are in-process, not timeline events (they are not session-scoped).

### D7: Formats pinned to the brief

prompts `*.md` (Go text/template, stem = name); agents `*.yaml` + `*.md` frontmatter; skills `<dir>/SKILL.md` with Agent Skills frontmatter; hooks `*.yaml` with the six-event enum and compiling matcher; mcps `*.yaml` stdio-or-HTTP variant. Hidden/editor temp files (dot-prefixed, `*.swp`, `~`-suffixed) are skipped by loaders and watcher alike.

## Risks / Trade-offs

- [fsnotify event semantics differ across platforms] → Debounce + classify-by-scan (after the window, re-read the path from disk — truth is the filesystem, not the event type) keeps behavior portable; tests poll with timeouts rather than asserting exact event sequences.
- [Hot-reload races a concurrent read] → Snapshot swap under atomic pointer; readers grab an immutable snapshot once.
- [Default config drift between bootstrap and docs] → Default file is marshalled from the typed defaults; docs show the marshalled output.
- [Watcher goroutine lifetime] → Same discipline as eventbus: context or Close ends it; leak test included.

## Migration Plan

New packages only; `go.mod` gains two deps. Rollback = delete `backend/internal/{config,registry}/` and `go mod tidy`.

## Open Questions

None material — debounce window (150ms) and subscriber callback shape are tunable later without touching the specs.
