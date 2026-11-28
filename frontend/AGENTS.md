# frontend/ — Svelte 5 Constitution

Scope rules for all work in `frontend/` (the pure SPA served in browsers and
embedded in the Wails v3 shell), on top of the root constitution. Target
stack: Svelte 5 + TypeScript + Vite + Tailwind, zero SSR.

## Svelte 5 Runes Only

Use `$state`, `$derived`, `$effect`, `$props`. **Never** legacy Svelte 4
reactive syntax (`$:` statements, `export let`). The SSE Timeline Tree store
in particular must be runes-based so 100+ tokens/sec streams render without
jank.

## FSD Import Rules

Layer direction, strictly one way:

```
app → features → entities → shared
```

- No cross-feature imports (`features/chat-stream` never imports
  `features/wards`).
- Layers only import downward: `app` may import `features`; `shared` imports
  nothing above it.
- Feature folders: `chat-stream`, `timeline-tree`, `wards`, `config-editor`,
  `settings`. Entities: `session`, `agent`, `memory-fact`, `timeline-event`.

## API Client Rule

All daemon access goes through the generated client in `src/shared/api/`
(regenerated from `contracts/openapi.yaml` via `task codegen`) and the
centralized SSE Timeline Tree store. No hand-rolled `fetch` calls to the
daemon, no per-feature API clients.

## Wails Integration Rule

- `frontend/` runs identically in a browser (`task dev`) and inside the Wails
  WebView (`task build:desktop`).
- Keep every `@wailsio/runtime` call behind `src/shared/platform/`; nothing
  outside that adapter may import Wails APIs.
- `vite.config.ts` uses `server.port = Number(process.env.WAILS_VITE_PORT) ||
  9245` with `strictPort: true`; static output lands in `frontend/dist/`.

- Delegation tools on the wire come in two shapes — the legacy dispatch
  tool `delegate_agent` (agent in the args) and the ADK spine's per-agent
  tools `delegate_<agent>` (agent in the name, `task` the only argument;
  engine-adk-spine D1). Never key a surface on the literal name: use
  `features/chat-stream/delegation.ts` (`isDelegationTool`,
  `delegationTarget`). The engine offers no tool for an agent whose name
  is not verbatim-safe (characters outside [A-Za-z0-9_-]), is literally
  `agent`, or collides with another — so a per-agent suffix is always the
  registry name verbatim (the skipped ones stay reachable as
  `delegate_agent {agent}`). `tests/delegation-tools.spec.ts` pins the
  per-agent shape (pills, slideout, Work nesting and turn boundary,
  Context roster); `run-legibility.spec.ts` keeps the legacy shape.

- `tests/_live/karta-live.mjs` is NOT an e2e spec (Playwright ignores the
  name): it drives the REAL UI against the live daemon (New Research → a
  goal → the run to completion), records every SSE frame the page sees,
  reads the transcript and session state back, and writes a report +
  screenshots to /tmp. `GOAL="…" node tests/_live/karta-live.mjs` from
  frontend/ — the operator's measurement runs (engine-adk-spine 2.5) use
  it; it costs real tokens on the configured default model.
  `tests/_live/karta-shots.mjs` screenshots an existing research session
  (thread + Work tab, slideout, Context tab, Settings → Agents) — the
  signoff evidence; no run is started.
