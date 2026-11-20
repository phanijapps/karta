# Proposal

## Why

z-Bot has frontend architecture rules but no runnable UI. Establish an Open WebUI-style desktop workspace so the shell, mock conversation, and configuration navigation can be reviewed before daemon integration.

## What Changes

- Scaffold a Svelte 5 + TypeScript + Vite + Tailwind SPA with the existing feature-layer boundaries.
- Closely match Open WebUI's desktop visual language: monochrome light/dark surfaces, compact navigation, restrained borders, centered chat, rounded composer, and a large settings dialog. Use Karta identity and independently authored components, not a generic dashboard redesign.
- Provide collapsible left navigation and right context panels, with explicit TODO placeholders for unimplemented destinations and Timeline/context content.
- Provide seeded mock chats, conversation selection, new-chat reset, and an in-memory composer with a deterministic mock reply.
- Provide settings sections for Providers, MCP, Skills, Logging, and Advanced. Use config-shaped illustrative data, editable in-memory drafts, and explicit non-persistence messaging.
- Add frontend development, check/build, and interaction-test task entrypoints; include frontend checks in `task verify`.
- Keep the entire shell usable without a daemon, provider credentials, network calls, or persistent browser storage.

## Capabilities

### New Capabilities

- `desktop-shell`: Desktop-style visual foundation, responsive two-sidebar workspace, theme switching, accessible navigation and settings overlay.
- `mock-chat`: Fixture-backed conversations and deterministic in-memory message composition without agent execution.
- `settings-shell`: Navigable configuration-shaped provider, MCP, skill, logging, and advanced previews with honest mock interaction states.

### Modified Capabilities

- `dev-workspace`: Add runnable frontend tooling and include frontend quality checks in repository verification without removing existing backend checks.

## Product Identity Update

The operator requested the app be rebranded to **Karta**. All visible app identity and current product documentation use Karta. Existing internal `zbot` names, API contract identifiers, and `~/.zbot/` storage paths remain compatible and are not renamed.

## Impact

- Future implementation touches `frontend/`, `Taskfile.yml`, and product/architecture documentation. No backend handlers, domain behavior, desktop Go wrapper, or API contract changes are needed.
- Introduces frontend dependencies, a package-manager lockfile, and offline fixture-based tests. Existing `task codegen` starts generating `frontend/src/shared/api/schema.d.ts` once `package.json` exists; that output remains derived solely from this repository's contract, although the shell does not call it.
- Reference: `https://github.com/open-webui/open-webui`, inspected at commit `0666e59d26a4d056e0bacee41742486badd71cf8` from a temporary clone outside this repository. Its license includes branding conditions; do not vendor its source, stylesheets, fonts, logos, or assets as part of this change.
- `/home/videogamer/projects/agentzero/contracts/**` supplies conceptual inspiration only. Its Rust-oriented schemas and endpoints are not z-Bot contracts or generated inputs.
- This change is shell-only. Provider/MCP connections, skill execution, real logging, filesystem configuration reads/writes, SSE, agent execution, authentication, uploads, and native Wails packaging are out of scope.
