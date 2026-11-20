# z-Bot — Architecture & Scaffolding Brief

## 1\. Executive Summary & Locked Stack

**z-Bot** is a local-first, autonomous, goal-oriented multi-agent desktop system. It separates a Go background daemon (`zbotd`) from multiple lightweight clients—a snappy **Svelte 5** web UI, a **Wails v3** native desktop wrapper, a **Bubbletea** TUI, and future messaging plugins (WhatsApp, Signal).

| Layer | Technology | Rationale |
| :---- | :---- | :---- |
| **Backend Language** | Go 1.26+ | Single-binary daemon, native concurrency, shared toolchain with Wails v3 and Bubbletea. |
| **Default Agent Engine** | CloudWeGo Eino (ADK \+ DeepAgents) | Go-native, built-in `FileSystem`/`Shell`, MCP adapter, `Skill` middleware, and `TaskTool` sub-agents—isolated behind `domain.AgentEngine` for zero lock-in. |
| **Compaction** | Custom Compaction Algorithm (`internal/compaction`) | First-class, engine-agnostic package plugged into Eino via a thin middleware adapter. |
| **API & Streaming** | `net/http` \+ `chi` \+ SSE (`contracts/openapi.yaml`) | Zero-boilerplate streaming for live Timeline Tree events to Svelte, Wails, and Bubbletea. |
| **Frontend UI** | Svelte 5 \+ TypeScript \+ Vite (Pure SPA) | No virtual DOM overhead for 100+ tokens/sec streaming; tiny bundle for Wails WebView. |
| **Native Desktop** | Wails v3 (`desktop/`) | 100% Go desktop shell with system tray, multi-window support, and embedded `frontend/dist`. |
| **CLI** | Bubbletea \+ Lipgloss \+ Cobra (`backend/cmd/zbot`) | Interactive terminal UI connecting to `zbotd` over the same local HTTP/SSE API. |
| **Memory & Storage** | SQLite \+ `sqlite-vec` \+ FTS5 \+ local ONNX | Single local `.db` file for sessions, Timeline Tree, hybrid semantic/keyword recall, and knowledge graph. |
| **Control Plane** | `~/.zbot/config/{prompts,agents,skills,hooks,mcps}/**` | Filesystem-first configuration with `fsnotify` hot-reload; zero hardcoded agents or tools. |

---

## 2\. Swappable Agent Engine Architecture (`domain.AgentEngine`)

To guarantee you can swap CloudWeGo Eino for Google ADK Go, a custom ReAct loop, or any future Go framework without rewriting the daemon, **no package outside `internal/engine/eino/` may import `github.com/cloudwego/eino`**.

### Core Domain Interfaces (`backend/internal/domain/`)

```go
package domain

import "context"

// AgentEngine abstracts the underlying orchestration framework (e.g., Eino, Google ADK, custom).
type AgentEngine interface {
	Name() string
	RunGoal(ctx context.Context, req GoalRequest) (<-chan TimelineEvent, error)
	Resume(ctx context.Context, sessionID string, input UserInput) (<-chan TimelineEvent, error)
	Cancel(ctx context.Context, sessionID string) error
}

// Compactor encapsulates your custom context-window compaction algorithm.
type Compactor interface {
	ShouldCompact(ctx context.Context, state ContextState) bool
	Compact(ctx context.Context, messages []Message, budget TokenBudget) ([]Message, CompactionStats, error)
}

// HookRunner executes Claude Code-style lifecycle hooks from ~/.zbot/config/hooks/**.
type HookRunner interface {
	Run(ctx context.Context, event HookEvent) (HookDecision, error)
}

// Channel is the Day-2 extension seam for messaging plugins (WhatsApp, Signal, Slack).
type Channel interface {
	Name() string
	Start(ctx context.Context, bus EventBus) error
	Stop(ctx context.Context) error
}
```

### How Eino Wires Into These Seams

- **`internal/engine/eino/`** implements `domain.AgentEngine` using Eino ADK (`adk.Runner`, `deep.New`, `WriteTodos`, `TaskTool`, `eino-ext` MCP tools, and `Skill` middleware).  
- **Swapping Engines Later:** Implement `internal/engine/<new-engine>/` against `domain.AgentEngine` and flip `engine: eino` in `~/.zbot/config.yaml`. Your memory store, custom compactor, hooks, Code Wards, Timeline Tree, Svelte UI, and Bubbletea CLI remain untouched.

---

## 3\. Pluggable Custom Compaction Layer (`internal/compaction`)

Your custom compaction algorithm lives in its own engine-agnostic package (`backend/internal/compaction/`), operating purely on `domain.Message` and `domain.TokenBudget`.

- **Adapter Pattern:** `internal/engine/eino/middleware_compaction.go` implements Eino's `ChatModelAgentMiddleware` (`BeforeModel` hook). Before each LLM call:  
  1. Converts Eino's message slice into `[]domain.Message`.  
  2. Calls `compactor.ShouldCompact(ctx, state)`.  
  3. If triggered, runs `compactor.Compact(...)`, updates Eino's message state, and emits a `CompactionTriggered` event (tokens before/after, strategy used) to `internal/eventbus` so it is visible in the Timeline Tree.  
- **PreCompact Hook:** Automatically fires any `PreCompact` hooks defined in `~/.zbot/config/hooks/**` right before compaction executes.

---

## 4\. Monorepo Directory Structure

```
zbot/
├── AGENTS.md                        # Root constitution (architecture, contracts, boundaries)
├── Taskfile.yml                     # Unified dev, codegen, test, lint, Wails v3 build
├── go.work                          # Go workspace linking backend/ and desktop/
├── contracts/
│   └── openapi.yaml                 # Single source of truth for daemon HTTP/SSE API
├── backend/                         # Go 1.26+ daemon & Bubbletea CLI
│   ├── AGENTS.md                    # Go, Eino isolation, compaction & hook rules
│   ├── go.mod
│   ├── cmd/
│   │   ├── zbotd/                   # Daemon entrypoint (HTTP/SSE + scheduler + channels)
│   │   └── zbot/                    # Bubbletea CLI entrypoint (connects to zbotd)
│   └── internal/
│       ├── domain/                  # Pure types & interfaces ONLY (zero internal imports)
│       ├── config/                  # ~/.zbot/config.yaml loader, defaults, env overrides
│       ├── registry/                # Loader + fsnotify hot-reload for ~/.zbot/config/**
│       │   ├── prompts/             # Go text/template .md fragments
│       │   ├── agents/              # Declarative agent specs (YAML / MD frontmatter)
│       │   ├── skills/              # <name>/SKILL.md + scripts
│       │   ├── hooks/               # Claude Code-style hook manifests
│       │   └── mcps/                # MCP server manifests (stdio / Streamable HTTP)
│       ├── eventbus/                # Pub/sub for Timeline Tree persistence & SSE streaming
│       ├── compaction/              # Custom compaction algorithm (implements domain.Compactor)
│       ├── hooks/                   # Claude Code-style hook executor (implements domain.HookRunner)
│       ├── engine/                  # Swappable agent engine implementations
│       │   └── eino/                # CloudWeGo Eino ADK + DeepAgent adapter (ONLY place importing eino)
│       ├── memory/                  # Local-first memory & learning engine
│       │   ├── store/               # SQLite + sqlite-vec + FTS5 + knowledge graph tables
│       │   ├── embedder/            # Local ONNX runtime wrapper
│       │   ├── recall/              # Hybrid vector+BM25 search + graph expansion
│       │   └── distillation/        # Async post-session transcript -> facts/entities
│       ├── tools/                   # Ward-scoped FS/Shell backends, MCP bridge, Bridge Workers
│       ├── wards/                   # Code Ward creation, naming, and path sandboxing
│       ├── channels/                # Future Day-2 plugins (whatsapp/, signal/) via domain.Channel
│       └── server/                  # net/http + chi + SSE handlers
├── frontend/                        # Snappy OpenWebUI-style SPA (Svelte 5 + TS + Vite)
│   ├── AGENTS.md                    # Svelte 5 runes, FSD, SSE & Wails rules
│   ├── package.json
│   ├── vite.config.ts               # Configured for WAILS_VITE_PORT (9245) + static dist/
│   └── src/
│       ├── app/                     # Shell layout, routing, theme, platform bootstrap
│       ├── features/                # chat-stream, timeline-tree, wards, config-editor, settings
│       ├── entities/                # session, agent, memory-fact, timeline-event
│       └── shared/                  # Generated API client, SSE store, platform adapter, UI primitives
├── desktop/                         # Thin Wails v3 native shell
│   ├── go.mod                       # Connects to local zbotd or embeds daemon service
│   ├── main.go                      # Wails v3 app, system tray, native menus, embeds frontend/dist
│   └── build/                       # OS icons, signing, installer configs
└── docs/
    ├── product/
    │   └── product-context.md
    └── architecture/
```

---

## 5\. Runtime Filesystem Control Plane (`~/.zbot/`) & Hooks

```
~/.zbot/
├── config.yaml                      # Engine selection, LLM providers, token budgets, wards root
├── data/
│   └── zbot.db                      # SQLite (sessions, timeline tree, sqlite-vec, FTS5, graph)
├── wards/                           # Persistent Code Ward project directories
└── config/
    ├── prompts/                     # *.md reusable system prompt templates
    ├── agents/                      # *.yaml specialist sub-agent definitions
    ├── skills/                      # <skill-name>/SKILL.md + optional scripts
    ├── hooks/                       # *.yaml Claude Code-style lifecycle hooks
    └── mcps/                        # *.yaml MCP server definitions
```

### Claude Code–Style Hooks Contract (`~/.zbot/config/hooks/*.yaml`)

```
event: PreToolUse                    # PreToolUse | PostToolUse | PreCompact | SessionStart | SessionEnd | SubagentStop
matcher: "execute|write_file"        # Optional regex matched against tool name
command: "~/.zbot/scripts/guard.sh"  # Receives JSON event payload on stdin
timeout_ms: 5000
```

- **Exit `0`:** Allow execution (optional JSON on `stdout` can modify args or inject context).  
- **Exit `2`:** Block execution and return `stderr` directly to the agent as a recoverable tool error.  
- **Other non-zero:** Log a warning event to the Timeline Tree and continue.

---

## 6\. Svelte 5 \+ Wails v3 Integration

- **Pure SPA (Zero SSR):** Scaffold `frontend/` with Vite (`svelte-ts` template) \+ Tailwind CSS. Output static assets to `frontend/dist/`.  
- **Svelte 5 Runes:** Use `$state` and `$derived` for the SSE Timeline Tree store so rapid token streams and nested sub-agent tool calls render at 60fps with minimal memory overhead.  
- **Wails v3 Dev & Build:** Configure `frontend/vite.config.ts` with `server.port = Number(process.env.WAILS_VITE_PORT) || 9245` and `strictPort: true`. Keep any `@wailsio/runtime` calls behind `src/shared/platform/` so `frontend/` runs identically in a browser (`task dev`) and inside the native Wails v3 window (`task build:desktop`).

---

## 7\. Scoped `AGENTS.md` Blueprints

### Root `AGENTS.md`

- Enforce local-first storage (`~/.zbot/data/zbot.db` \+ `~/.zbot/config/**`).  
- Enforce contract-first API development (`contracts/openapi.yaml` \-\> `task codegen`).  
- Enforce channel-agnostic core (no UI, CLI, Wails, or WhatsApp/Signal imports inside core packages).  
- Require every agent step, delegation, tool call, hook decision, and compaction event to emit to `internal/eventbus`.

### `backend/AGENTS.md`

- **Engine Isolation Rule:** Only `internal/engine/eino/` may import `github.com/cloudwego/eino`. All other packages depend exclusively on `domain.AgentEngine`.  
- **Compaction Rule:** Compaction logic belongs in `internal/compaction/` implementing `domain.Compactor`, never hardcoded inside an engine adapter.  
- **Downward Dependency Graph:** `domain` \-\> (`config`, `registry`, `eventbus`) \-\> (`compaction`, `hooks`, `memory`, `tools`, `wards`) \-\> `engine/*` \-\> `server` & `channels`.  
- **Testing:** Table-driven unit tests, in-memory SQLite (`:memory:`), and fake LLM/engine stubs—never hit live APIs in unit tests.

### `frontend/AGENTS.md`

- **Svelte 5 Only:** Always use Svelte 5 runes (`$state`, `$derived`, `$effect`, `$props`). Do not mix legacy Svelte 4 reactive syntax (`$:`, `export let`).  
- **FSD Import Rules:** `app` \-\> `features` \-\> `entities` \-\> `shared`. No cross-feature imports.  
- **API Client:** Use only the generated client in `src/shared/api/` and the centralized SSE Timeline Tree store.

---

## 8\. Day-1 Scaffolding Sequence

1. **Workspace & Contracts:** Initialize `go.work`, `Taskfile.yml`, `contracts/openapi.yaml`, `backend/internal/domain`, and `backend/internal/eventbus`.  
2. **Config & Registry:** Implement `~/.zbot/config.yaml` and `~/.zbot/config/{prompts,agents,skills,hooks,mcps}` loaders with `fsnotify` reload.  
3. **Compaction & Hooks:** Implement your custom `domain.Compactor` in `internal/compaction/` and the Claude Code-style `domain.HookRunner` in `internal/hooks/`.  
4. **Eino Engine Adapter \+ Bubbletea CLI:** Wire `internal/engine/eino/` (`deep.New`, Ward-scoped FS/Shell, MCP, Skills, Compaction & Hook middlewares) and stream events to `cmd/zbot`.  
5. **Memory & Distillation:** Add SQLite \+ `sqlite-vec` \+ FTS5 \+ local ONNX embedder \+ post-session distillation pipeline.  
6. **Svelte 5 Frontend \+ Wails v3 Desktop:** Scaffold `frontend/` (Chat \+ Timeline Tree \+ Config inspector) and wrap with `desktop/` Wails v3 system tray.