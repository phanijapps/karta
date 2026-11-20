# Karta Product Context

> The single maintained source of product truth for Karta. Every agent making a
> product-level decision consults this document first. The founding architecture
> brief is preserved at [`docs/architecture/architecture-brief.md`](../architecture/architecture-brief.md);
> where later evolution makes the two disagree on a product matter, this document wins.

## Vision

Karta is a **local-first, autonomous, goal-oriented multi-agent desktop system**.
A single Go daemon (`zbotd`) runs autonomous agent work toward user goals, and
multiple lightweight clients — a Svelte 5 web UI, a Wails v3 native desktop
shell, and a Bubbletea TUI — observe and steer that work over a local HTTP/SSE
API. Everything persistent stays on the user's machine: one SQLite database,
one filesystem control plane, zero cloud dependency.

The product promise: an agent system you *own* — goals in, observable autonomous
work out, all state inspectable and local.

## Target Users

- **Developers and power users** who want an autonomous agent workspace on their
  own machine, with full visibility into what the agent is doing (Timeline Tree),
  full control over its configuration (filesystem control plane), and no data
  leaving the device.
- **Tinkerers** who want to swap agent engines, write skills, define sub-agents,
  and add lifecycle hooks without rebuilding the system.
- **Future:** users reachable through messaging channels (WhatsApp, Signal) once
  the Day-2 channel seam ships.

## Core Concepts

- **Daemon/Client split** — `zbotd` (Go) owns agents, memory, and state; every
  client (web, desktop, TUI, future channels) talks to it over the same local
  HTTP/SSE contract. The core is channel-agnostic.
- **Swappable agent engine** — the orchestration framework (default:
  CloudWeGo Eino ADK + DeepAgents) is isolated behind the `domain.AgentEngine`
  seam. Engines are swapped by configuration, never by rewriting the daemon.
- **Custom compaction** — an engine-agnostic context-window compaction algorithm
  (`internal/compaction`) plugged into the engine as middleware; every
  compaction is visible as an event.
- **Timeline Tree** — the persistent, streaming record of every agent step,
  delegation, tool call, hook decision, and compaction event; the backbone of
  observability, streamed to all clients via SSE.
- **Code Wards** — sandboxed project directories where agents do file/shell
  work, with path sandboxing enforced by the ward layer.
- **Filesystem control plane** — `~/.zbot/config/{prompts,agents,skills,hooks,mcps}`
  plus `~/.zbot/config.yaml`: zero hardcoded agents or tools, hot-reloaded via
  fsnotify. The user's files *are* the configuration.
- **Claude Code–style hooks** — lifecycle hooks (`PreToolUse`, `PostToolUse`,
  `PreCompact`, `SessionStart`, `SessionEnd`, `SubagentStop`) with exit-code
  semantics (0 allow, 2 block, other non-zero warn-and-continue) that let the
  user guard and steer agent behavior.
- **Local-first memory & learning** — SQLite + sqlite-vec + FTS5 + local ONNX
  embeddings: hybrid semantic/keyword recall and a knowledge graph, distilled
  from sessions asynchronously after they end. No external embedding API.

## Non-Goals

- **Not cloud-hosted / not multi-tenant SaaS** — no server-side deployment
  story, no shared accounts, no remote storage of user data.
- **Not a mobile product** — clients are web, desktop (Wails), and terminal.
- **No cloud LLM lock-in at the product layer** — providers are configured, not
  baked in; and the agent engine is swappable by design.
- **No server-side team features** (shared workspaces, RBAC, org management).
- **Day-2 channels (WhatsApp, Signal, Slack) are not Day-1** — the `Channel`
  seam exists, the plugins do not yet.

## Product Identity

The user-facing app name is **Karta**. The wordmark, assistant identity, composer label, settings identity, and browser title use Karta. Existing `zbot` package names, daemon/CLI identifiers, API schemas, and `~/.zbot/` paths remain unchanged; this is a visual rebrand, not a storage or compatibility migration. Historical architecture and specification records may retain the former z-Bot name.

## Current Frontend Preview

The Svelte SPA now provides an Open WebUI-style desktop shell: collapsible left navigation, mock conversations and composer, a right Context/Timeline placeholder, and a Settings dialog with Providers, MCP, Skills, Logging, and Advanced sections. Light/dark themes and responsive drawers work in memory.

**Providers is live and manageable**: the section reads the daemon's providers over the HTTP contract and can add, edit, and delete them — daemon-managed files under `~/.zbot/config/providers/`, applied immediately without a restart. API keys entered in the form are stored once in the OS keyring (Linux Secret Service, macOS Keychain, Windows Credential Manager) and never shown again; an unavailable keyring fails with typed guidance (verified end-to-end against a local ollama and a real keyring, including rename re-key and delete cleanup). Providers declared in `~/.zbot/config.yaml` stay read-only with a config.yaml badge and restart-applied semantics. Without a daemon the section shows an explicit connection state, never fixtures.

The header model picker is now live: it reads the daemon's in-memory model catalog (`GET /api/models`) — grouped by provider, refreshed manually (Settings → Providers Test/Refresh; nothing polls), selection labeled simulated. The rest remains **shell-only**: replies are fixtures; Settings drafts (MCP, Skills, Logging, Advanced) are discarded on close. Logging previews proposed fields, not supported daemon configuration. MCP/Skills previews do not reflect the installed registry. No engine run, chat completion, filesystem, or SSE connection is made; no browser state is persisted. The Wails desktop wrapper and live session integration remain future work, as does using configured providers inside agent runs (the engine adapter consumes them later). See [frontend setup](../../frontend/README.md), [shell architecture](../architecture/frontend-shell.md), and the provider-slice verification in `openspec/changes/add-openai-compatible-providers/evidence.md`.

## Maintenance

This document changes with the product: any change that alters product-level
behavior, concepts, or non-goals updates this file in the same change. Pure
internal refactors do not.
