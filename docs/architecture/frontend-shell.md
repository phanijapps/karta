# Frontend shell architecture

## Current boundary

The frontend is a pure Svelte 5/TypeScript/Vite SPA with Tailwind available and CSS semantic tokens for its visual foundation. It is a browser-compatible shell, not a native desktop wrapper or a live agent client. See `frontend/README.md` for runnable commands. The source of truth for future daemon access remains `contracts/openapi.yaml` and its generated client types.

## Design contract

Mode: create. Surface genre: workspace. Supported minimum width: 375 CSS pixels. The primary job is exploring the local agent workspace before connecting it to a daemon. The first screen shows navigation, a mock transcript, model selection, and the composer. The right panel is available but closed initially. The shell has no loading or network-error state because it makes no requests; content, empty/new chat, empty search, disabled integrations, and TODO destinations are explicit states.

Aesthetic reference: Open WebUI desktop, commit `0666e59d26a4d056e0bacee41742486badd71cf8`. Its source organization favors focused UI components and feature-specific settings; its README provides concrete installation and operating instructions. We apply those practices within Karta's required `app → features → entities → shared` architecture rather than copying its SvelteKit application, API/auth stores, or legacy reactivity.

Design handoff: no `[design]` section configured in repository or user-profile layout configuration. The explicit operator-supplied Open WebUI reference governs. XD genre routing: skipped (experience-design pack absent). No external handoff artifact was consumed.

Reference-derived visual choices: zero-chroma neutral surfaces, system sans typography, compact navigation, rounded 58rem-max composer, and a large rounded settings dialog with 240px section navigation. Independently authored Karta extensions: 260px left panel / 44px rail and 300px right panel. No Open WebUI source, stylesheet, font, logo, or asset is vendored. Lucide supplies bundled local icons. See the frontend third-party notice.

Primitive tokens feed semantic light/dark aliases, then component styles. Shared controls use named classes; feature CSS stays scoped. Geometry is intentionally explicit in the owning component instead of an opaque global utility component. Avoid duplicated theme palettes or unbounded generic stores.

## Responsibilities

- `src/app/App.svelte` owns page composition, theme, desktop panel preferences, active drawer, and the settings overlay. It closes drawers on breakpoint transitions. App-level Sidebar and ContextPanel know only their own presentation and callbacks.
- `src/features/chat-stream/Chat.svelte` owns the composer draft and message presentation. Typed sample conversations live in `src/entities/session/`. The entity factory owns in-memory selection and transcript mutations; fixture model choices never initiate discovery.
- `src/features/settings/Settings.svelte` owns the selected section and drafts. `fixtures.ts` supplies only illustrative values. No chat feature imports settings or vice versa.
- `src/shared/ui/Modal.svelte` owns native dialog lifecycle, explicit Tab wrapping, Escape/backdrop dismissal, and focus restoration. Dialog consumers supply their own content.
- `src/shared/api/schema.d.ts` is generated and never manually edited; `src/shared/api/client.ts` is the sole module that calls the daemon (generated types only, no hand-rolled bindings). No platform adapter is needed until a native capability exists; no Wails package is imported.

## Layout and keyboard behavior

At widths of 1280px and above, both panels can dock. At 768–1279px, the left panel docks and the right uses a drawer. Below 768px, both use drawers. The active drawer is one discriminated state, so two modal drawers cannot coexist. Crossing a breakpoint closes any drawer and clears modal/inert state; desktop panel preferences survive.

Settings closes a drawer before opening, so focus traps never stack. Keyboard Tab/Shift+Tab stay inside the active dialog; Escape and backdrop dismiss it. Focus returns to the invoker; if a drawer invoker has been removed, restore to the stable left-panel toggle. Section navigation uses ordinary buttons and `aria-current`, not a partially implemented ARIA tab pattern. Icon-only controls have accessible names. Focus uses a 2px contrasting outline with 3px offset. Motion is opt-in under `prefers-reduced-motion: no-preference`.

The header and composer model pickers (`shared/ui/ModelPicker.svelte`) read the daemon's model catalog (`GET /api/models`, manual-refresh semantics — no fixture fallback; empty and daemon-down states are explicit). Enter sends unless IME composition is active; Shift+Enter inserts a newline. Blank input cannot submit. Sending appends one deterministic reply immediately and retains composer focus. User text is escaped by Svelte interpolation. A small polite live region announces the reply, not the entire transcript.

## Configuration preview truth

Providers is a live, manageable daemon view (see add-openai-compatible-providers and manage-model-providers): it lists providers from both sources with origin badges (`Managed` files under `config/providers/` vs read-only `config.yaml` entries), supports add/edit/rename/delete with immediate effect, stores API keys once in the OS keyring (never under `~/.zbot/`, never echoed), and its Test connection proxies model discovery through the daemon. Offline it shows an explicit daemon-connection state, never fixtures.

MCP, Skills, Logging, and Advanced remain honest previews: isolated fixture lists and illustrative fields, clearly labeled, with inert source-path text. Logging remains a proposed UI-only field the daemon does not accept; Advanced illustrates actual engine/server/compaction/wards concepts without loading current values. Drafts for these sections survive section changes but are discarded on every close; Reset preview only restores fixtures. No fixture section claims Save/Apply, connect, install, live-tail, export, restart, config opening, or actual reset. Further integration must first extend the local API contract, regenerate bindings, then implement handlers and clients; never promote fixture shapes into hand-written clients.

## Data and verification

No localStorage, sessionStorage, IndexedDB, cookies, analytics, external fonts, or SSE is used. The only HTTP calls are the daemon-contract requests from `shared/api/client.ts` (Providers listing and model discovery via the dev proxy); mock surfaces make none. The page does not access `~/.zbot/`. Sample data is not a hardcoded production agent/tool registry. No real agent work occurs, so no fabricated eventbus emission is appropriate.

`task check:frontend` checks types/components and builds `frontend/dist`; `task test:frontend` runs deterministic Playwright behavior and axe checks. Both are included in `task verify`, alongside existing Go checks. Visual capture is separate from judgement: `pnpm build && node tests/capture.mjs` in `frontend/` creates images and a capture manifest; observations and known limitations belong in `frontend-shell-evidence.md`.
