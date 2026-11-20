# Karta frontend verification evidence

## Scope and routes

Pilot-tier, shell-only workspace at `/`, including the Settings dialog on the same route. No production agent/configuration integration is claimed. The latest change rebrands the visible app as Karta while preserving internal `zbot` identifiers and `~/.zbot/` paths.

## Viewports and browsers

Declared channels: `>=375 <768`, `>=768 <1280`, and `>=1280`. Supported minimum: 375. Discarded breakpoints: none. Captures use widths 375, 768, and 1280 with heights 600 and 900, plus 1440×900 and 375×812. Chromium 159 via Playwright was tested; Firefox, Safari, and the Wails WebView were not.

## States

Seeded conversation, new/empty conversation, message submission, blank and multiline input, IME composition, model preview, search/no results, collapsed sidebar, docked panels/drawers, placeholder destinations, settings sections/drafts/reset/discard, both themes, disabled integrations, and keyboard dismissal/focus restoration.

## Screenshots

32 production-build captures are stored in `frontend-shell-captures/`. `manifest.json` records each route, surface, viewport, theme, attained scroll offset, and measured page-scrollability. The outer page does not scroll at any captured viewport; conversation and settings content use internal scrolling. Reproduce with `pnpm build && node tests/capture.mjs` from `frontend/`. Captures were last refreshed after the OpenAI-compatible providers change (which made Settings → Providers a live daemon view): the offline preview shows that section's explicit daemon-connection state, by design, instead of provider fixtures.

## Inspection observations

Before rebranding, all 32 views were inspected through light/dark contact sheets: neutral navigation, readable chat, rounded composer and dialog, and distinct narrow settings navigation. Short viewports intentionally scroll the transcript/settings internally rather than the outer page. After rebranding, all 32 captures were refreshed; the 1440×900 dark shell and light Settings images were inspected directly. Karta is legible in the sidebar, assistant identity, and Settings footer, without changing layout geometry.

Current rebrand visual spot-check: completed/pass for those two desktop captures. A fresh complete-set post-rebrand visual judgement remains unverified; capture generation alone is not claimed as a complete visual pass. The pinned Open WebUI source informed the styling; no pixel-identical comparison is claimed.

Final close-out inspection (task 5.2): this session's reviewing model cannot view images, so the design conformance pass was executed programmatically against `design.md`'s measurable criteria on the production build (dark theme; 1440×900 and 375×812): composer capped at 58rem (measured 902px) with 24px radius; dialog 32px radius; 13px navigation font; system font stack; 2px solid focus outline with 3px offset; no outer-page scroll at 1440×900; no horizontal overflow at 375×812 (shell and Settings); Providers renders the configured live provider through the preview proxy with a real daemon, and its explicit daemon-unreachable state offline. Result: 11/11 PASS. Direct image judgement of the refreshed set remains a recorded gap, not silently claimed.

## Accessibility result

Playwright axe checks pass for shell and Settings in both themes using `wcag2a`, `wcag2aa`, and `wcag21aa`. Tests verify keyboard trapping/restoration and enabled visible button targets of at least 24×24 CSS pixels. Focus styling uses a 2px contrasting outline and 3px offset, but a full manual WCAG 2.4.13 AAA focus-appearance evaluation is outstanding. WCAG 2.2 AA coverage gaps remain for 2.4.11, 2.5.7, 3.2.6, 3.3.7, and 3.3.8; this is not full WCAG conformance certification.

Rendered HTML validation passes across all 32 captures. Runtime-serialization/format preferences and implicit default button/input type rules are disabled in the capture validator; semantic and ARIA rules remain enabled. Form submit buttons have explicit types.

## Performance result

Latest static build: JS 91.41 kB (32.09 kB gzip), CSS 22.76 kB (4.82 kB gzip). No remote fonts or image dependencies. Mobile/desktop CWV and Lighthouse have not been measured; bundle size is not a substitute for runtime performance evidence.

## Console/network result

Offline interaction tests reject non-local and daemon API requests, verify no local/session storage, IndexedDB databases, or cookies, and confirm reload resets transient state. Dedicated console-error assertions have not been added. Runtime integrations are unavailable rather than silently attempted.

## Analytics events

Not applicable: analytics and telemetry are intentionally absent in this local-only shell.

## Verification commands

- `task verify`: PASS after Karta implementation, including contract codegen, Go vet/gofmt, golangci-lint (0 issues), race tests, backend build, frontend check/build, and 9 Chromium tests.
- `pnpm check`: 0 errors and 0 warnings.
- `node tests/capture.mjs`: 32 captures; rendered HTML validation PASS.
- `pnpm audit --prod`: no known vulnerabilities in the prior shell check; the rebrand introduced no dependency changes.
- Codegen idempotence: verified before the rebrand; API contract/generated types were not changed by branding.
- Existing contract lint warnings: missing API license metadata and a recommended operation 4xx response. Contract validation passes; these are pre-existing, not silently fixed here.

Final close-out commands (task 5.3, after the providers change): `task verify` green end-to-end (codegen drift-free, lint 0 issues, `go test -race` all packages, svelte-check 0 errors/0 warnings, clean build, **14/14** Chromium tests — the suite grew with the live-Providers specs); `task codegen` idempotent (byte-identical regeneration); FSD import direction, no hand-rolled daemon access outside `src/shared/api/client.ts`, no Wails imports anywhere, and unchanged backend boundaries re-verified by the close-out review below.

## Known exceptions and unverified items

Close-out review (task 6.1) completed: the project's `zbot-reviewer` agent is not loadable in this harness session (roster lists user/builtin agents only), so the builtin read-only reviewer ran the identical zbot-reviewer checklist over the current working tree (a superset of this change — the later providers change was in flight). Verdict: OK with notes — no P0; 2 P1 (stale `frontend-shell.md` passages; the desktop-shell "no daemon HTTP" clause conflicting with the successor change's live Providers view) and 5 P2, **all fixed before archive**: doc rewritten to the live-Providers truth, the spec clause scoped to mock surfaces (with a close-out note in the delta), tasks reconciled, dangling `aria-controls` made conditional, theme initialized from `matchMedia` synchronously (no dark flash), the drafts footer scoped to fixture sections, and the root README refreshed. Post-fix `task verify` green (14/14 tests) and `task codegen` byte-stable (task 6.2).

Remaining unverified items: full cross-browser and manual assistive-technology testing, direct image judgement of the refreshed capture set (programmatic conformance pass recorded above instead), runtime performance measurements (CWV/Lighthouse), and WCAG 2.4.11/2.5.7/3.2.6/3.3.7/3.3.8 coverage — recorded gaps, not silently claimed.

## Security/privacy and reliability status

Not applicable at production tier: this is a pilot shell. No independent security/reliability sign-off is claimed. User text and settings previews remain in memory; no real credentials should be entered. The initial secret-pattern scan found no matches. Reload restores fixtures; there are no runtime requests or recovery promises for future integrations.
