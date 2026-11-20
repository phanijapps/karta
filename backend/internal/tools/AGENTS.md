# tools

Karta's tool seam: small named units with a description, a parameters
schema, and an exec function. The chat and engine layers consume tools
exclusively through `Provider` implementations; skills, MCP servers,
and subagent tools register additional providers later without consumer
changes. This package ships the two builtins (clock.now, echo) —
primitive functions of the daemon, not registry content.

- `Tool` interface: `Def()` (name/description/params) + `Exec(ctx, args)`.
- `Provider` interface: `Tools()` returns the offerable set.
- `Execute` runs by name across a provider; unknown tools return a
  typed error result the model can see (never panic, never secrets).
- Pure stdlib; no internal imports below it in the graph.
