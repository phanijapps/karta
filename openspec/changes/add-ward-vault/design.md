# Design

## Context

Ward-based research exploration (docs/architecture/ward-research-
exploration.md) names five slices; this is the foundation. Operator
decisions recorded: default vault `~/Documents/wards` (OS-aware);
the execution toolchain for ward work is **uv for Python** (per-ward
venvs), **node/npx on demand**, and shell execution through Karta's
existing tool plane — NOT a port of agentzero's Rust shell machinery.

## Investigation findings (what we build on)

**eino's tool surface (v0.9.23 in go.mod):** `components/tool` defines
BaseTool/InvokableTool/StreamableTool + `utils.InferTool` (schema
inferred from Go funcs). **eino-ext (our version) ships only
`components/model` — no shell/python tool components.** Conclusion: a
"eino shell tool" to leverage is the *interface pattern*, not a library;
Karta's `internal/tools.Tool` (Def/Exec) already IS that seam and the
adapter already converts it to eino's schema. Ward tools are therefore
Karta-native tools in/behind `tools.Provider`, consuming eino's pattern
— zero new framework dependencies.

**agentzero's execution family** (`runtime/agent-tools/src/tools/
execution/`): shell.rs (platform shell, blocked-command list, timeout),
write_file/edit_file, ward_cwd.rs (resolves the ACTIVE WARD as cwd;
fallback `~/Documents/zbot/wards/<id>` — same Documents convention we
adopted). Its skills call `uv pip install ...` directly — the per-task
ephemeral-env pattern. We adopt the *shape* (cwd scoping, guardrails,
uv) in Go.

**uv**: fast Python env manager; `uv venv` + `uv pip install` +
`uv run python` cover per-ward isolated envs without global pollution;
absent uv degrades to system python (documented).

## Goals / Non-Goals

**Goals:**
- Vault resolution (OS-aware default, config override), bootstrap seeds.
- Ward-conf.yaml schema parse + linter (typed tree, agentzero's shape
  simplified), archetype templates (generic, research) seeded user-owned.
- Read-only API + Wards surface (replacing the Code Wards placeholder).
- The foundation must not need rework when ward tools land (slice 3):
  the linter's node ids, the archetype confs, and the API tree shape are
  the contract slices 2-4 build on.

**Non-Goals (slices 2-5):** intent analysis, ward tools/sticky ward,
shell/python/node execution, spec→plan→tasks skills, chitta bridge.

## Decisions

1. **Vault root resolution** mirrors agentzero's convention:
   `dirs.Documents()/wards` (Go: os.UserHomeDir + "Documents" on
   Linux where xdg-user-dirs may be absent; Documents on win/mac),
   overridable via `config.yaml wards.vault_root`. Absolute paths only;
   `~` expanded. Recorded as the constitution's second scoped
   `~/.zbot` exception (user-chosen local data, like the keyring rule).
2. **`internal/wards` package** (not a registry kind): wards live in
   user space, not the control plane. Package surface:
   `Open(vaultRoot)`, `Index()` (scan → []WardInfo), `Lint(ward)`,
   `ParseConf(raw)`, archetype template assets embedded. The registry
   stays config-tree-only; a thin `wards` field on the daemon wires it.
3. **Layout schema** = agentzero's declarative tree, trimmed: node
   {id?, kind: file|dir, match, required?, repeat?, format?:
   markdown|raw, children?, $ref?}. Strict decode; unknown fields are
   typed errors. Lint walks the actual tree against the rules: missing
   required, unexpected entries, format mismatch → typed findings with
   node ids. No $ref in v1 lint (parse only) unless trivially cheap.
4. **Archetype templates** embedded (go:embed) like example agents:
   generic (freeform: canonical + AGENTS.md + log only) and research
   (spec/plan/tasks subtrees + pages/sources/data/outputs roles — the
   conf the operator's flow skills will write into). Seeded into the
   vault on first bootstrap; user-owned forever after.
5. **API shape anticipates slice 3**: the ward view carries
   `archetype`, `lint` findings, `canonical_title`; the layout tree
   endpoint returns node ids + matches (the same ids the ward tool will
   inject as the Active Ward Template).
6. **Tool-execution strategy recorded for slice 3** (not built here):
   ward-scoped shell/python/node tools as Karta `tools.Tool`s —
   cwd pinned to the active ward, agentzero-derived blocked-command
   list + output caps + timeout; Python via per-ward `uv` venv
   (`~/.venv` inside the ward, gitignored), `uv run python -c/-m`;
   node via `node`/`npx` on demand; absent runtime = typed tool error
   naming the install path. The eino boundary holds: tools live in
   Karta's plane; the adapter already converts.

## Risks / Trade-offs

- [Documents dir absent on headless Linux] → fall back to ~/Documents,
  create it; the default is a default, config overrides.
- [Vault outside ~/.zbot surprises the constitution] → scoped exception
  recorded in the same change; local-first's substance (no cloud, no
  remote state) is untouched.
- [Schema over-modeling] → v1 lints only required/unexpected/format;
  $ref parse-only unless free.
- [Ward explosion / index cost] → scan on open + fsnotify later; v1
  scans per request (vaults are small).

## Migration Plan

Additive; nothing moves. First run seeds `~/Documents/wards/` (or the
configured root) with two archetype template dirs under `.templates/`
+ an empty `.gitignore` starter guidance. Rollback = revert; the vault
is inert user data.

## Open Questions

- Expose lint findings in the list view or a per-ward detail only?
  (Leaning: summary badge in list, findings on detail.)
- Watch the vault (hot index) in v1 or scan-per-request? (Leaning:
  scan-per-request; hot-reload with slice 3's sticky ward.)
