# registry

File-driven registry over the control plane: prompts, agents, skills,
hooks, and MCP manifests loaded from the config tree into immutable
snapshots, plus the write paths (CRUD) for the two managed kinds —
MCPs and skills. Imports `domain` (hook event names) and stdlib +
yaml/fsnotify; sits beside `config` in the dependency graph — the
daemon wires `config.Paths` into `registry.Dirs`, registry never
imports config.

- Nothing hardcoded: an empty config tree is an empty registry with zero
  errors. One bad file never blocks the rest — failures are per-file
  `LoadError`s carrying paths.
- Readers get whole snapshots (`atomic.Pointer` swap on reload); no reader
  ever sees partial state. `Errors()` returns the latest per-file load
  errors, coherent with the current snapshot (swap-despite-errors: an
  entry whose file no longer loads is absent, its error names the file).
- Formats: prompts `*.md` (Go text/template, stem = name); agents
  `*.yaml` + `*.md` frontmatter (body = system prompt); skills
  `<dir>/SKILL.md` (Agent Skills frontmatter); hooks `*.yaml` (six-event
  enum, compiling matcher, required command); mcps `*.yaml` (stdio
  and/or streamable-http — at least one transport, both is a valid
  dual-transport server). Skill frontmatter decodes into a DTO whose
  `path` field is a sink: authored paths are ignored, `SkillSpec.Path`
  is always the directory walk's value. Duplicate names (agents, hooks,
  mcps, skills) are reported and the lexicographically first file wins.
  Kinds are capacity-bounded (mcps 128, skills 256): overflow files get
  load errors, so API lists are complete within the bound.
- Write paths (mutate.go + mutate_mcp.go / mutate_skill.go / schedules.go /
  mutate_files.go): `Create/Update/Delete` for MCPs and skills
  under the reload mutex, serialized with watcher flushes. All file
  writes are atomic (temp+rename, never truncating in place); creates
  and rename targets install NO-CLOBBER (hard link for files,
  renameat2(RENAME_NOREPLACE) for directories on Linux — see
  noclobber_*.go for the guarded fallback), so occupied paths fail
  `duplicate_name` even when the occupant is invisible to the snapshot.
  After any failed filesystem step the kind is reloaded from disk and
  THAT is published (disk truth wins). Managed names
  (`^[a-z0-9][a-z0-9-]{0,63}$` minus Windows reserved) are required
  only of API create/rename targets — legacy stems and `.yml` files are
  editable in place via their loaded source path; renames normalize
  (an MCP rename retry resumes over its own byte-identical target).
  `CreateMCPFromKey` is the import create: `Slugify` (the one
  normalizer, exported) turns the pasted key into the stem and
  duplicates (snapshot name, occupied target path, same-paste slug
  collisions) are checked BEFORE capacity.
  Skill edits are full replaces (body required) preserving bundles;
  skill rename moves the directory with rollback compensation; skill
  delete removes the whole directory (bundles included, by design).
  `SkillDetail` is a pure read (no publishing side effects).
- The single publication path (`reloadKindLocked`) emits the WHOLE-KIND
  diff: mutation results, external edits absorbed by a racing reload,
  and failure residuals each notify exactly once; no-diff reloads (a
  mutation's own watcher settle) emit nothing.
- Hot-reload (`Watch`): recursive fsnotify, 150ms path-keyed debounce,
  classify by re-reading disk, whole-kind rebuild + diff, subscriber
  callbacks (via `Registry.Subscribe`). Registry changes are in-process
  notifications, not timeline events.
- Key entrypoints: `New`, `LoadAll`, `Snapshot`, `Errors`, `Subscribe`,
  `Watch`, `CreateMCP`/`UpdateMCP`/`DeleteMCP`,
  `CreateSkill`/`UpdateSkill`/`DeleteSkill`/`SkillDetail`.
- Schedules (schedules kind): cron-expressed goal runs under config/schedules — loader + CRUD in schedules.go; capacity 128
- `AgentSpec.RequireToolUse` (`require_tool_use: true`, optional, strict
  decode in both forms): the engine nudges a run under the agent once
  when its first answer uses no tools — the research and builder seeds
  declare it (delegation-truthfulness).
- System-role agents (`system.go`): `IsSystemAgent` holds the reserved
  host-run names (`intent-analyst`). They are ordinary roster entries
  (Settings lists them; the file is user-owned) but the engine never
  offers them for delegation and the intent wrapper never lists them in
  its own catalog packet.

