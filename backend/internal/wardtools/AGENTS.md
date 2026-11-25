# internal/wardtools

Ward hosting for root goal runs (slice 3, the mini-bot model): the
ward_use / ward_create tools, the per-session hosting state they
mutate, and the Active Ward packet every entry returns — a hosted run
executes as the ward's own agent, under its constitution (AGENTS.md) +
layout template + digest.

## Responsibility

- `Host` — the session's sticky-ward state (thread-safe; read by the
  engine for children and for the ## Active Ward section on hosted
  runs). `Active()` returns a read-only `WardView`;
  `RefreshDigest()` detects concurrent ward edits (warn-only).
  `SetOnEnter` wires the post-enter observer (the engine adapter binds
  it to the session-row ward binding — add-chat-modes); `Reenter` is
  the wiring-side enter (daemon restart recovery from the binding).
- `UseTool` / `CreateTool` — root-only structural tools. ward_use:
  exact index match (typed unknown error with the roster); the RESULT
  carries the Active Ward packet (mid-run entry can't retro-edit the
  system prompt — the packet IS the injection). ward_create:
  instantiate from `.templates/<archetype>/` (canonical starter
  renamed to `<name>.md`, write-if-absent copies, post-create lint
  must be clean), then enter.
- `Packet(view)` — the packet renderer (constitution + layout +
  digest, untrusted-data framing).

## Boundaries

- Imports wards + domain + tools (peer/lower tiers); NEVER engine,
  server, or client code. The VAULT stays read-only except
  ward_create's one-time instantiation.
- File tools (ward_read/write/list) extend this package over the same
  hosting state (task 2.2).

## Key entrypoints

`NewHost`, `(*Host).Active`, `(*Host).RefreshDigest`, `UseTool`,
`CreateTool`, `Packet`.

- `Host.Leave` clears the active ward (leave-ward): `Active()` is nil
  and the file/shell tools refuse until re-entered. The binding
  observer is the daemon's to update (it wires Leave on the endpoint).

## Containment + digest (add-ward-hosting close-out)

- `nofollow_unix.go` / `nofollow_other.go`: `readNoFollow` /
  `writeNoFollow` open with `O_NOFOLLOW` (unix) — a FINAL-component
  symlink is refused at open time (no Lstat-then-open window); any
  symlink is refused (the layout is a tree of real files). An outside
  file can never be read or overwritten through the ward tools.
- The hosting digest fingerprints the RAW bytes of ward-conf.yaml
  (`wards.Conf.Raw`) + AGENTS.md — any external edit changes it; every
  ward tool (read/write/list) reports a mismatch as a warning.
- `Host.enterKind(…, created)` reports a typed `Transition` (previous
  ward captured under the lock) → the adapter emits `ward_entered` /
  `ward_created` (persist then publish).
