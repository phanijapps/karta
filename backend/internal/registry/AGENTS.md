# registry

File-driven registry over the control plane: prompts, agents, skills,
hooks, and MCP manifests loaded from the config tree into immutable
snapshots. Imports `domain` (hook event names) and stdlib + yaml/fsnotify;
sits beside `config` in the dependency graph — the daemon wires
`config.Paths` into `registry.Dirs`, registry never imports config.

- Nothing hardcoded: an empty config tree is an empty registry with zero
  errors. One bad file never blocks the rest — failures are per-file
  `LoadError`s carrying paths.
- Readers get whole snapshots (`atomic.Pointer` swap on reload); no reader
  ever sees partial state.
- Formats: prompts `*.md` (Go text/template, stem = name); agents
  `*.yaml` + `*.md` frontmatter (body = system prompt); skills
  `<dir>/SKILL.md` (Agent Skills frontmatter); hooks `*.yaml` (six-event
  enum, compiling matcher, required command); mcps `*.yaml` (stdio xor
  streamable-http). Skill bundles are tracked by their directory
  (`SkillSpec.Path` is the verbatim bundle root for later injection).
  Duplicate names (agents, hooks, mcps, skills) are reported and the
  lexicographically first file wins.
- Hot-reload (`Watch`): recursive fsnotify, 150ms path-keyed debounce,
  classify by re-reading disk, whole-kind rebuild + diff, subscriber
  callbacks. Registry changes are in-process notifications, not timeline
  events.
- Key entrypoints: `New`, `LoadAll`, `Snapshot`, `Watch`, `OnChange`.
