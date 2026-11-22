# Change summary — add-chitta-engine-tools

## What landed

- `tools.Multi` (kernel): merged tool surface; duplicate names panic at
  COMPOSITION time (eager validation — final-gate P1: the panic used to
  lurk in Tools() until the first request).
- `chitta/engine.go`: `System.ToolProvider(fallback)` binds the seven
  chitta tools onto the kernel `tools.Provider` seam; `HostContextKey`
  carries session/execution/agent identity from the calling layer;
  arguments never carry provenance.
- Closed-system degradation: `Close()` marks the system; chitta tool
  calls return typed `unavailable` while builtins keep working.
- `cmd/zbotd`: chat + engine constructed with
  `tools.Multi(BuiltinProvider, chittaTools)` — agents reach chitta
  mid-conversation (9 unique tools).

## Review (zbot-reviewer)

P1 fixed: eager composition-time collision validation in Multi.
P2s fixed: dead `var _ = strings.TrimSpace` + unused import;
kernel-import lists (store.go comment + AGENTS.md) now include
`internal/tools`; zbotd AGENTS tool-plane note added; product-context
sentence moved to Core Concepts; merged-surface exactly-nine-unique
test; exactly-one-event assertion tightened.
Observation recorded (pre-existing, out of scope): `Dispatch` reads
`s.store` outside `s.mu` vs `Close` under it — benign today
(close-at-shutdown only); the distillation change (which adds
concurrent callers) should snapshot the store under the mutex.

## Verification

Race suites green (fake + real LadybugDB incl. binding tests),
golangci 0 issues, builds clean, openspec valid, 7/7 tasks.
