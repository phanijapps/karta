# Tasks

## 1. Kernel: merged provider

- [x] 1.1 Add `tools.Multi(providers ...Provider) Provider` to `internal/tools` (duplicate tool names panic at composition; unknown-name execution keeps the typed error). Verify: table tests — merged enumeration, duplicate-name panic, unknown tool error unchanged.

## 2. Chitta binding

- [x] 2.1 Add `backend/chitta/engine.go`: `System.ToolProvider(fallback domain.ChittaHostContext) tools.Provider` exposing the seven tools (schemas from `ToolDef`), `chittaTool.Exec` → `System.Dispatch` → `tools.Result`; export `HostContextKey` for context-stamped identity (missing fields filled from fallback; arguments never carry provenance). Verify: adapter tests — all seven tools enumerated with schemas; a `memory_remember` call through the provider persists with fallback provenance and emits one event; context-stamped session/execution flows into provenance; invalid args return `invalid_input` in `tools.Result.Err` (spec scenarios "Agent calls memory_remember", "Provenance stamped from the execution context", "Anonymous fallback", "Invalid chitta tool input").
- [x] 2.2 Degradation test: close the System; provider calls return the unavailable error while a builtin tool still executes through the same `Multi` (spec scenario "Closed chitta does not take the tool plane down").

## 3. Composition and docs

- [x] 3.1 Wire `cmd/zbotd`: `tools.Multi(tools.BuiltinProvider{}, chittaSys.ToolProvider(...))` passed to chat + engine; update `backend/chitta/AGENTS.md` (binding entrypoint + HostContextKey contract) and the zbotd AGENTS note. Verify: `go build ./cmd/zbotd` clean; grep shows chat/engine constructed with the merged provider.
- [x] 3.2 Update `docs/product/product-context.md`: memory tools are agent-reachable (one sentence in the Current State). Verify: sentence present, no stale "mock-only" claims about tool surface.

## 4. Verification

- [x] 4.1 Full: `go test -race` (skillhub excluded as pre-existing WIP), `CHITTA_DRIVER=ladybug` suite, golangci-lint zero, boundary greps (chitta imports only kernel packages downward; no kernel package imports chitta; eino isolation untouched). Verify: all green in one run.

## 5. Review and hardening

- [x] 5.1 Run the read-only zbot-reviewer subagent over the change scope; fix every P0/P1; fix P2s or record why not in the change summary.
- [x] 5.2 Re-run `task verify` after review fixes; mark complete only when green.
