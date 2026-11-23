# internal/wards

The ward vault: user-owned directories of documented knowledge living
OUTSIDE `~/.zbot` (default `~/Documents/wards`, overridable via
`config.yaml` `wards.vault_root`). This package is the foundation of
ward-based research (slice 1 of `docs/architecture/ward-research-exploration.md`).

## Responsibility

- **Vault resolution** — `ResolveVaultRoot(configured, daemonHome)`:
  OS-aware `~/Documents/wards` default, `~` expansion, absolute-only
  (relative is a typed `VaultRootError` naming the config field), and
  the constitution's vault exception ENFORCED — roots equal to or
  beneath the resolved daemon home are rejected, symlink aliases
  included (resolved through the deepest existing ancestor).
- **Bootstrap** — `Bootstrap(root)`: create the vault, seed archetype
  templates (`.templates/{generic,research}/`, go:embed) + `.gitignore`
  starter. Write-once: hand edits survive every re-bootstrap.
- **Index** — `Vault.Index()`: scan wards (name, archetype, conf
  presence, lint summary, canonical title). Conf-less wards are valid
  freeform wards (archetype `generic`).
- **Conf schema** — `ParseConf(raw, source)`: strict decode of the
  trimmed declarative tree (`kind: file|directory`, `match`, `required`,
  `repeat`, `format: markdown|raw`, `children`, `$ref` parse-only);
  unknown fields and semantic violations are typed `ConfParseError`s
  naming the field.
- **Linter** — `Lint(dir, name, conf)`: walk a ward's tree against its
  conf → typed findings (`missing_required`, `unexpected_entry`,
  `format_mismatch`; `{ward}` expands to the ward name; dot-entries are
  tolerated).

## Boundaries

- Imports stdlib only (no `internal/` deps today) — sits in the
  `compaction/hooks/memory/tools/wards` tier of the dependency graph;
  NEVER imports `engine/*`, `server`, or client code.
- The vault is USER DATA: after bootstrap this package only reads it.
  Ward creation/mutation belongs to ward tools (slice 3) or the user.
- Like `internal/registry`, the package owns plain filesystem scans;
  I/O entry points still take `ctx` first (go-rules 2.2) and check
  cancellation between entries — individual os calls are not
  cancellable, boundaries are. No eventbus emission — read-only listing,
  no agent steps, tool calls, or hook decisions happen here.

## Key entrypoints

`ResolveVaultRoot`, `DefaultVaultRoot`, `Bootstrap`, `Open` →
`(*Vault).Index()` / `(*Vault).Detail(name)`; `ParseConf`; `Lint`.
