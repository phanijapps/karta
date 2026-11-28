# config

Control-plane foundation: home resolution, first-run bootstrap, strict
`config.yaml` loading. Imports stdlib + `yaml.v3` + `internal/noclobber` (the shared no-replace move); sits above `domain`
in the dependency graph (nothing here imports registry/eventbus/engine).

- Resolution precedence: `--home` flag > `ZBOT_HOME` env > `~/.zbot`. The
  winner relocates the whole root. All paths derive from `config.Paths`.
- Bootstrap is create-only: default `config.yaml` written with `O_EXCL`
  (never overwrites), registry dirs, idempotent; fresh and pre-marker homes seed the example roster once — six agent examples under ~/.zbot/agents and five prompt-craft skills under the top-level ~/.zbot/skills (seedassets.go: marker-gated, crash-atomic link publish; after the marker, deletions stick; never over a migration or user files).
- Loading is strict and hard-fails: unknown keys, invalid YAML, and absent
  files are errors naming the file — never a fallback to defaults.
- `providers:` (optional list, absent = empty): one entry per model
  provider — `name` (unique), `type` (only `openai-compatible` today),
  `base_url` (absolute http(s), the provider's API root used **verbatim**,
  version prefix included, e.g. `http://localhost:11434/v1` — consumers
  append only their path), and optional `api_key_env` (an environment
  variable NAME; the daemon reads the value at request time — secret
  values are never stored under `~/.zbot/`). The keyring marker is NOT
  valid in this file (files under `config/providers/` are the managed
  source; see internal/providers). Invalid entries fail startup naming
  the file and entry.
- `server.address` must be host:port. Loopback is the default and
  recommended bind (the daemon is unauthenticated); a wildcard (0.0.0.0/::)
  or LAN address is an explicit operator choice that exposes the
  unauthenticated API to the network — the request guard adapts (see
  internal/server) and the operator owns that trust decision.
- Key entrypoints: `ResolveHome`, `NewPaths`, `Bootstrap`, `Load`, `Default`.
## Layout notes (relocate-skills-hooks)

- Skills load from `~/.zbot/skills/`, hooks from `~/.zbot/hooks/`
  (top-level; `Paths.SkillsDir`/`HooksDir`). `config/{skills,hooks}`
  are legacy: `migrateLegacySkillsHooks` (bootstrap, BEFORE ensureDirs)
  moves entries up one-by-one, never overwriting, hand-move-aware.
- Skill seeds target `paths.SkillsDir`; prompt seeds stay in
  `config/prompts/` (daemon config, not user skills).
- The honesty-contract upgrade (delegation-truthfulness): the
  research-agent and builder-agent seeds carry `require_tool_use: true`
  and a "never describe a step you did not take" clause; homes seeded
  before it upgrade ONCE under `.agent-seeds-honesty`
  (`upgradeAgentSeedsHonesty`) and only when the user's copy is still
  byte-identical to the embedded pre-honesty seed
  (`seedassets/agents_v1/`) — edited copies are never touched.
- `delegation.max_depth` (config, default 1) is the operator's hold on
  nested delegation; the daemon applies it with `SetDepthCap`.
- The intent analyst (intent-analyst-agent S1) seeds from
  `seedassets/system-agents/` into `~/.zbot/agents/intent-analyst.md`
  under its OWN marker (`.intent-analyst-seeded`, `seedIntentAnalyst`
  runs first in `seedExamples`): homes already marked by the roster
  change receive it on upgrade exactly once; afterwards edits and
  deletions stick like every other seed.

- The context budget knobs live in `context_budget.go` (engine-adk-spine
  slice 1): `skills.injection` (`progressive` default | `bodies`),
  `compaction.tool_result_cap` (bytes, default 12 KB), and
  `compaction.summarizer` (optional provider/model; empty = the run's own
  model). Absent keys default; unknown values fail loudly. The builder
  seed carries `reasoning_effort: low` and upgrades once per home from
  its byte-identical post-honesty copy (`.agent-seeds-effort`;
  `upgradeAgentSeeds` is the shared once-per-home rewrite).
