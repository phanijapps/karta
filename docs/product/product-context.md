# Karta Product Context

> The single maintained source of product truth for Karta. Every agent making a
> product-level decision consults this document first; the forward capability
> queue lives in [`backlog.md`](backlog.md). The founding architecture
> brief is preserved at [`docs/architecture/architecture-brief.md`](../architecture/architecture-brief.md);
> where later evolution makes the two disagree on a product matter, this document wins.

## Vision

Karta is a **local-first, autonomous, goal-oriented multi-agent desktop system**.
A single Go daemon (`zbotd`) runs autonomous agent work toward user goals, and
multiple lightweight clients — a Svelte 5 web UI, a Wails v3 native desktop
shell, and a Bubbletea TUI — observe and steer that work over a local HTTP/SSE
API. Everything persistent stays on the user's machine: one SQLite database
for conversations and the Timeline, one LadybugDB (chitta) for memory and
knowledge, one filesystem control plane, zero cloud dependency.

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
- **Wards** — the ward vault: user-owned directories of documented
  knowledge OUTSIDE `~/.zbot` (default `~/Documents/wards`), each
  self-describing via an optional `ward-conf.yaml` layout schema with a
  linter; read-only in the UI today. (The old "Code Wards" sandboxed-
  execution concept lives on as the slice-3 tool-plane direction:
  ward-scoped execution with optional Docker isolation.)
- **Filesystem control plane** — the top-level
  `~/.zbot/{agents,skills,hooks}/` and
  `~/.zbot/config/{prompts,mcps,providers}/`, plus `~/.zbot/config.yaml`: zero hardcoded
  agents or tools, hot-reloaded via fsnotify. The user's files *are* the
  configuration. A fresh install seeds an example roster (six agent examples
  + five prompt-craft skills, user-owned after first write) — examples, not
  built-ins: delete or edit them freely.
- **Claude Code–style hooks** — lifecycle hooks (`PreToolUse`, `PostToolUse`,
  `PreCompact`, `SessionStart`, `SessionEnd`, `SubagentStop`) with exit-code
  semantics (0 allow, 2 block, other non-zero warn-and-continue) that let the
  user guard and steer agent behavior.
- **Chitta — local-first memory & learning** — Chitta (चित्त, the
  mind-field) is the core the harness uses to continuously evolve: one
  LadybugDB (graph + full-text + vector) holds durable memory and the
  knowledge graph of the user's world in `~/.zbot/data/chitta/`, with
  hybrid recall; conversations are distilled into it asynchronously by
  a backend job (never written directly). Local fastembed BGE embeddings
  (bge-small-en-v1.5, 384-dim); no external embedding API;
  `zbot.db` stays SQLite for sessions and the Timeline Tree.

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

The header model picker is live: it reads the daemon's in-memory model catalog (`GET /api/models`) — grouped by provider, refreshed manually (Settings → Providers Test/Refresh; nothing polls), selection labeled simulated. Settings → MCP and Skills are live managed views of the daemon's registry: MCP servers (stdio command/args/env and/or HTTP url — both transports on one entry is valid) add/edit/rename/delete with immediate effect and a server-side connection probe per declared transport (discovered tools, or typed spawn/connect/timeout/protocol failures); skills add/edit/rename/delete as `SKILL.md` directories (body round-trips, bundles preserved, deletion confirms the bundle). Env values are manifest file content — the keyring stays the only secret store; the daemon validates a trusted loopback Host on every request. Broken hand-edited files surface as per-file load errors. Logging and Advanced remain **shell-only** previews: drafts discarded on close. No engine run consumes MCP tools or skills yet (the tools seam reserves them); no SSE connection is made; no browser state is persisted. The Wails desktop wrapper and live session integration remain future work, as does using configured providers inside agent runs (the engine adapter consumes them later). See [frontend setup](../../frontend/README.md), [shell architecture](../architecture/frontend-shell.md), and the provider-slice verification in `openspec/changes/add-openai-compatible-providers/evidence.md`.

## Maintenance

This document changes with the product: any change that alters product-level
behavior, concepts, or non-goals updates this file in the same change. Pure
internal refactors do not.
