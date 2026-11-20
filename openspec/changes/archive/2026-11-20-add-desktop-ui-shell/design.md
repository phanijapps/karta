# Design

## Context

See `proposal.md` for motivation and the capability deltas for observable behavior. The operator confirmed shell-only scope and requested Open WebUI's desktop styling as-is in feel, rather than a loose thematic inspiration.

Observed repository state:
- `frontend/` contains only `AGENTS.md`; there is no package manifest, source, build configuration, or test runner.
- Frontend rules require Svelte 5 runes, TypeScript, Vite, Tailwind, pure SPA rendering, downward feature-layer imports, generated daemon bindings, and Wails isolation.
- `Taskfile.yml` currently verifies only contracts and the backend. Its codegen task conditionally generates TS schema types once `frontend/package.json` exists.
- `backend/internal/config/config.go` currently defines engine, server address, compaction token budgets, and wards root. Providers and logging are not existing config fields. Do not present their previews as valid daemon YAML.
- `contracts/openapi.yaml` defines health, goals, sessions, cancel/resume, and session events, not settings endpoints. This change needs no API additions.

Reference evidence:
- Open WebUI clone: `/tmp/zbot-open-webui-reference`, commit `0666e59d26a4d056e0bacee41742486badd71cf8`. Reproduce later with the public repository URL and pinned commit; the temporary directory is not a deliverable.
- `src/tailwind.css`: neutral OKLCH grayscale and system-first sans typography.
- `src/lib/components/layout/Sidebar.svelte`: compact 13px text, neutral background, subtle separators, icon rail, and expanded navigation.
- `src/lib/components/chat/MessageInput.svelte`: centered maximum 58rem composer, 24px rounding, subtle border/shadow, dark neutral elevated surface.
- `src/lib/components/chat/SettingsModal.svelte`: near-full-window rounded dialog, 15rem navigation column, compact section buttons, separate content scrolling.
- This was source inspection, not a claim of having run or visually tested upstream.
- AgentZero `session-details.yaml`, `mission-control.yaml`, and `work-surfaces.yaml` informed only the distinction between conversation, context, and future work surfaces. No endpoints, schemas, or Rust implementation are adopted.

## Goals / Non-Goals

**Goals:** Establish a reusable visual foundation and independently testable in-memory shell. Make the current mock boundary unmistakable without scattering oversized warning cards across the chat.

**Non-Goals:** No daemon bootstrap, API requests, real settings schema, runtime registry seeding, browser persistence, real agent events, Wails Go wrapper, or SSR. UI fixture messages and Timeline placeholders are not agent execution and do not emit fabricated eventbus records.

## Decisions

### 1. Independently implement the Open WebUI desktop vocabulary

Aesthetic reference: Open WebUI desktop at the pinned commit above. Preserve the requested look through geometry, typography, neutral surfaces, and interaction density, while writing Karta components independently. Do not copy upstream source/CSS/assets or rebrand a vendored application: the inspected license includes branding conditions. Use a small permissively licensed icon package and system fonts, with license notices as needed. Do not introduce remote font or icon delivery.

Alternative rejected: importing Open WebUI or translating its SvelteKit application. That introduces backend/auth dependencies, legacy reactive patterns, unrelated features, and licensing obligations beyond this shell.

Product identity: Karta in the document title/description, visible wordmark, assistant name, settings footer, and composer label. Use K in the collapsed rail/avatar. Keep internal `zbot` names, contract identifiers, and `~/.zbot/` source-path previews unchanged; no migration is included.

Visual contract (reference-derived unless marked as a legacy z-Bot extension):

| Area | Target |
| --- | --- |
| Desktop frame | Full-height workspace; no marketing header, gradients, metric cards, or dashboard chrome |
| Left navigation | z-Bot extension: 260px expanded / 44px rail; 13px text; 32–36px rows; compact monochrome icons; Settings anchored at bottom |
| Main chat | Minimum-width-zero center; header about 56px; readable transcript and composer capped at 58rem; user bubbles and unboxed assistant text |
| Composer | 24px radius; faint border/shadow; neutral elevated surface; compact round send button; no large colored CTA |
| Right context | z-Bot extension: 300px; closed initially to preserve the reference's chat focus; tabs for Context and Timeline with TODO content |
| Settings | 32px outer radius; 32px desktop viewport inset; 240px left section list; subtle divider; independently scrolling details |
| Light theme | White main canvas, near-white navigation, charcoal text, faint gray separators |
| Dark theme | Very dark navigation, slightly lighter main canvas, elevated composer/dialog; off-white text; low-contrast structural borders, legible controls |

Use primitive → semantic → component CSS custom properties. Starting primitive anchors: neutral lightness 0.98, 0.94, 0.77, 0.69, 0.51, 0.32, 0.27, 0.24, 0.20, 0.16 with zero chroma; map roles rather than scattering raw colors across components. System sans stack starts with `-apple-system`, `BlinkMacSystemFont`, `Segoe UI`, then sans-serif. Use 13px navigation, 14px controls, and 15–16px conversation text. Preserve contrast and visible focus even where upstream styling is subtler.

Initial semantic seed (aliases, not separate per-component palettes):

```css
:root {
  --ds-color-background: var(--neutral-white);
  --ds-color-sidebar: var(--neutral-98);
  --ds-color-surface: var(--neutral-white);
  --ds-color-text: var(--neutral-20);
  --ds-color-muted: var(--neutral-51);
  --ds-color-border: var(--neutral-94);
  --ds-radius-control: 0.5rem;
  --ds-radius-composer: 1.5rem;
  --ds-radius-dialog: 2rem;
  --ds-chat-width: 58rem;
}
[data-theme='dark'] {
  --ds-color-background: var(--neutral-20);
  --ds-color-sidebar: var(--neutral-16);
  --ds-color-surface: var(--neutral-24);
  --ds-color-text: var(--neutral-98);
  --ds-color-muted: var(--neutral-69);
  --ds-color-border: var(--neutral-32);
}
```

### 2. Compose at the app layer, keep mock state below it

Proposed source responsibilities:

```text
src/app/                    entry, App, shell layout, sidebar orchestration, theme
src/features/chat-stream/   chat view, composer, model preview
src/features/settings/      dialog, section navigation, in-memory drafts, fixtures
src/entities/session/       fixture conversations, mock models, runes-based session state
src/shared/ui/              buttons, dialog, drawer, icons, empty/placeholder panels
src/shared/styles/          primitive/semantic tokens and global base styles
src/shared/api/             generated schema.d.ts only; unused by mock runtime
src/shared/platform/        browser-safe seam only if required; no native dependency
```

Use `.svelte.ts` for shared runes state where needed. The app composes chat and settings; neither feature imports the other. Settings provider drafts do not mutate the chat model fixtures, since doing so would imply integration. Avoid a universal settings/chat/global store and avoid abstractions for APIs that do not exist yet.

Alternative rejected: a monolithic App component and feature-to-feature imports. Both would make subsequent daemon integration harder and violate the established layer direction.

### 3. Define the interaction state contract now

| Surface | States and transitions | Unavailable behavior |
| --- | --- | --- |
| Left navigation | Expanded, rail, or narrow drawer; sample selection; New Chat; Wards/config placeholders | No real sessions or filesystem navigation |
| Chat | Seeded transcript or empty greeting; draft; append user message plus fixed mock reply | No artificial streaming spinner, provider call, tool run, or fabricated success |
| Model selector | Fixture list, selected value, explicit mock indication | No discovery or connection |
| Right panel | Closed/open; Context/Timeline selection; TODO content | No actual Timeline Tree or source inspection |
| Settings | Closed/open; selected section; draft retained on section switch; reset; discard on close | No Save/Apply success, connection tests, install, export, restart, or file access |
| Theme | System preference on first load; in-memory light/dark override | No persisted preference |

Settings opens Providers by default. Place a compact persistent “Shell preview — changes are not saved” notice in the dialog. Use read-only or disabled controls with adjacent explanation for integration-only actions; avoid silent clickable no-ops. Providers preview label, type, base URL, model identifiers, and a credential environment-variable name, never a real key. MCP and Skills list fixed examples, clearly unrelated to installed registry content. Logging previews level/diagnostics; Advanced previews the existing engine/server/compaction/wards concepts. Show source paths as inert text, not file links. Any proposed provider/logging field must be labeled as a preview, not downloadable/importable YAML.

All state is page-lifetime memory. Discard settings drafts on every dialog close; retain chat transcripts until reload. For chat, append a deterministic reply immediately rather than building a fake scheduler or token-stream subsystem. Render messages as text, not raw HTML.

### 4. Responsive layout and keyboard contract

Use CSS grid/flex with `min-width: 0`, bounded scroll areas, `100dvh`, and wrapping long message text. At 1280px and above, both sidebars can be docked; from 768–1279px, the left stays docked and the right uses a drawer; below 768px, both use drawers. Only one drawer can be active. Desktop sidebar preferences are independent from the active drawer state.

Prefer the native modal dialog primitive for Settings and narrow overlays with explicit focus setup/restoration and tested dismissal. Opening Settings closes any sidebar drawer first. Resize transitions must close modal drawer state before switching to a docked panel. Escape dismisses the active overlay; background is inert; no two competing focus traps. Use normal navigation buttons with `aria-current` for settings sections, avoiding an incomplete ARIA tab pattern. Give every icon-only button an accessible label and sidebar toggles `aria-expanded`/`aria-controls`. Keep logical focus order, visible focus rings, and reduced-motion handling.

Composer contract: Enter sends except during IME composition; Shift+Enter inserts a newline; blank Send is disabled; focus stays in the composer after sending. Use a polite, limited announcement for the new mock reply rather than repeatedly announcing the whole transcript.

Alternative rejected: keeping two squeezed sidebars on small screens or relying on visual hover states for navigation.

### 5. Tooling and verification are part of the scaffold

Use pnpm with a committed lockfile and pinned package-manager version, Vite Svelte plugin, Svelte 5, TypeScript, Tailwind, and svelte-check. Configure `server.port = Number(process.env.WAILS_VITE_PORT) || 9245`, `strictPort: true`, and `build.outDir = 'dist'`. Keep the output static and browser-safe; no SvelteKit dependency or Wails runtime imports.

Add Playwright interaction tests against the local preview/dev server. Keep them deterministic, with no external requests; document installing the Chromium test runtime. Add `task dev`, `task check:frontend` (check + build), and `task test:frontend`; append frontend checks/tests to existing `task verify` without weakening backend checks. Existing codegen will now generate `src/shared/api/schema.d.ts`; retain generated-file ownership, run codegen twice, and check byte-for-byte stability.

Test matrix: navigation and sidebar independence; desktop/narrow resize transitions; conversation switching/new chat; Enter/Shift+Enter/IME/blank input; inert HTML; all settings sections and draft discard; both themes; keyboard focus trapping/restoration; no network/storage side effects. Capture shell and settings screenshots in both themes at 1440×900 plus shell/settings at 375×812; inspect the reference characteristics above and record viewport, results, and known gaps in a frontend evidence note. Screenshot generation alone is not a visual pass.

## Risks / Trade-offs

- [Visual fidelity is subjective; upstream can change] → Pin the reference commit and use the explicit geometry/token checklist plus reviewed screenshots. Do not claim pixel-identical reproduction without measured evidence.
- [Upstream licensing includes branding restrictions] → Independently authored implementation; no vendored code/assets. Reassess licensing before any future direct reuse.
- [Config previews could look authoritative] → Mark proposed fields and source paths as illustrative; no real YAML import/export or save success.
- [Mocks could become accidental production defaults] → Keep fixture modules isolated and visibly labeled; do not seed config files, registry entries, or daemon state.
- [Frontend scaffold activates codegen] → Generate only from local `contracts/openapi.yaml` and verify stable output.
- [Browser tests add setup cost] → Document the Chromium prerequisite, use local deterministic fixtures, and do not silently skip the tests in verification.
- [No native shell yet] → Preserve browser/WebView-compatible static assets and adapter boundaries; Wails compilation remains a separate change.

## Migration Plan

No persisted state or API migration is involved. Introduce the frontend scaffold, shell/features, tests/tasks, and documentation in reviewable increments. Verify the static build without a daemon. Rollback removes the scaffold and its task additions without touching `~/.zbot/` or backend behavior. Keep the maintained product/architecture docs explicit that this is a mock shell rather than delivered live agent functionality.
