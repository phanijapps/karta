# domain

Pure types and interface seams for the whole daemon. **Zero internal
imports, stdlib only** — enforced by `purity_test.go`; nothing else in the
repo may be imported here, and every other backend package may import this
one.

- One seam per file: `engine.go` (AgentEngine), `compaction.go` (Compactor),
  `hooks.go` (HookRunner), `bus.go` (EventBus/Subscription),
  `channel.go` (Channel), plus their exchanged types in `event.go` and
  `session.go`.
- Change types here only via an OpenSpec change; engines, the server, and
  the contract all compile against these shapes.
- Never add behavior, framework imports, or configuration to this package.
