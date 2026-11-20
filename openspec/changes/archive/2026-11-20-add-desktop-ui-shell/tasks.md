# Tasks

## 1. Frontend scaffold and tooling

- [x] 1.1 Create the Svelte 5 + TypeScript + Vite + Tailwind pure SPA scaffold under `frontend/`, pnpm scripts and pinned package-manager version, lockfile, and downward-layer source directories; verify a frozen-lockfile install, `pnpm check`, and `pnpm build` succeed without a daemon.
- [x] 1.2 Configure the strict development port using `Number(process.env.WAILS_VITE_PORT) || 9245`, static `frontend/dist` output, and local-only assets; verify default/override port startup, occupied-port failure, and that the built SPA runs without SSR or Wails dependencies.
- [x] 1.3 Add Playwright with a local server configuration and documented Chromium prerequisite; add a smoke test proving the scaffold loads without a daemon and verify the test passes.
- [x] 1.4 Add `task dev`, `task check:frontend`, and `task test:frontend`, preserve existing backend/contract verification, and include frontend checks/tests in `task verify`; document commands and prerequisites in `frontend/README.md` and verify each new entrypoint runs.
- [x] 1.5 Run `task codegen` to produce `frontend/src/shared/api/schema.d.ts` solely from the local contract; run it again and verify byte-for-byte stable output, with no AgentZero-generated inputs or runtime API wiring.

## 2. Visual foundation and workspace shell

- [x] 2.1 Implement primitive/semantic/component tokens and light/dark in-memory themes using the Open WebUI reference geometry and neutral palette in `design.md`; verify both themes on the scaffold and record the pinned reference and independently authored/no-vendored-assets boundary in `docs/architecture/frontend-shell.md`.
- [x] 2.2 Build the full-height app frame, 260px left navigation/44px rail, centered chat slot, and independent initially closed 300px right panel; implement New Chat/fixture navigation slots, bottom Settings entry, Wards/config placeholders, and Context/Timeline TODO panels; verify selected states and sidebar independence with interaction tests.
- [x] 2.3 Implement docked/drawer breakpoints, single-drawer ownership, dismissible overlays, and resize cleanup; verify 375×812, 1024×768, and 1440×900 layouts, no horizontal page overflow, and overlay-to-desktop transitions in Playwright.
- [x] 2.4 Add named icon controls, expanded/current states, focus-visible styles, overlay focus containment/restoration, Escape/backdrop dismissal, and reduced-motion behavior; verify keyboard flows and absence of stranded inert/focus states through browser tests.
- [x] 2.5 Document the delivered layout and shell-only limits in `docs/architecture/frontend-shell.md`; verify the documented panel defaults, breakpoints, and TODO destinations match the interaction tests and implementation.

- [x] 2.6 Rebrand the visible app as Karta in the browser title/description, full and collapsed wordmarks, assistant identity, composer label, Settings, and current docs; preserve internal identifiers and storage paths; verify a dedicated browser branding test and refreshed screenshots.

## 3. Mock chat experience

- [x] 3.1 Add isolated session/model fixtures and runes-based page-lifetime state with at least two sample conversations, selection, and a new empty chat; verify each conversation retains its own transcript while switching and New Chat preserves prior chats.
- [x] 3.2 Implement the compact header and mock model selector, safe text messages, empty greeting, rounded composer, and immediate deterministic mock reply; verify Enter sends, Shift+Enter adds a newline, IME Enter does not send, whitespace is rejected, latest content is revealed, and composer focus is retained.
- [x] 3.3 Mark model selection and replies as mock; omit or explain unavailable attachment/voice/share/execution actions; verify no connection or tool action occurs and HTML/script-like user content remains inert.
- [x] 3.4 Update `docs/product/product-context.md` with the mock-chat preview status and deferred live-agent integration; verify the documentation makes no claims of real sessions, execution, or streaming.

## 4. Settings shell

- [x] 4.1 Build the rounded desktop settings dialog with 240px section navigation, responsive narrow layout, independent content scroll, Providers default selection, and persistent non-persistence notice; test all five sections and focus restoration, including opening Settings from a sidebar drawer.
- [x] 4.2 Add fixture-backed Providers list/detail drafts for label, type, base URL, model identifiers, and credential environment-variable reference; label proposed schema fields and inert `~/.zbot/config.yaml` source hint; verify no secret-value entry, config read, discovery, or connection test exists.
- [x] 4.3 Add MCP and Skills fixture lists/details with transport/command/URL and `SKILL.md` path previews; verify explicit mock statuses and unavailable connect/install/file actions without process, network, or filesystem access.
- [x] 4.4 Add Logging level/diagnostics previews and Advanced engine/server/compaction/wards previews; verify editable drafts and explanations for unavailable live-tail, export, restart, real reset, and file access.
- [x] 4.5 Implement draft retention across section switches, reset-to-fixtures, and discard-on-close through every dismissal path; verify reopening restores original fixtures and no Save/Apply success is emitted.
- [x] 4.6 Document settings state ownership, existing versus proposed configuration concepts, and future contract-first integration in `docs/architecture/frontend-shell.md`; update product context with settings preview limits and verify it matches the supported sections.

## 5. Integrated acceptance and visual evidence

- [x] 5.1 Add an integrated offline test that blocks non-local runtime requests, detects daemon/SSE/provider attempts, checks browser storage/cookies, and reloads the shell; verify chat/settings/theme/navigation work and all transient state resets without persistent writes or telemetry.
- [x] 5.2 Capture and inspect shell and Settings in both themes at 1440×900 plus narrow shell/Settings at 375×812; record screenshots and results in `docs/architecture/frontend-shell-evidence.md`, comparing navigation density, neutral surfaces, typography, 58rem chat/composer width, rounded geometry, focus visibility, and scrolling against `design.md`; fix mismatches or explicitly record remaining gaps.
- [x] 5.3 Run `task check:frontend`, `task test:frontend`, codegen idempotence, and `task verify`; inspect FSD import direction, no hand-rolled daemon access, no Wails imports outside the platform seam, and unchanged backend boundaries; verify all pass and record command outcomes in the evidence note.

## 6. Review and hardening

- [x] 6.1 Run the read-only `zbot-reviewer` subagent over the implementation, these change artifacts, and constitution boundaries; fix every P0/P1 finding, fix P2s or record why deferred, and verify findings are resolved with targeted tests and the review summary.
- [x] 6.2 Re-run `task verify` after the final review fixes and evidence/documentation edits; verify it is green with no generated drift or unrelated files, and record the final result before declaring implementation complete.
