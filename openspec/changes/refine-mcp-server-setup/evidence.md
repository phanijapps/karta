# MCP setup refinement — design review record (pre-implementation)

## gpt-6.1-sol design review (zbot-reviewer subagent)

Verdict: **BLOCK — 4 P1, 5 P2**; boundaries, contract-first sequencing,
D7 eventbus reasoning, transport-selector UX, and security baseline
confirmed correct. All 9 findings folded:

- **P1 object transport destroyed paste semantics** → the paste now
  travels as raw text; the daemon is the only parser (token walk,
  json.Number, paste order incl. integer-like keys, exact-text numeric
  env coercion); duplicate keys — detectable in the walk, escaped
  equivalents included — reject the whole document before any write
  (design D1, mcp-import "Raw-text document handling").
- **P1 shape ladder vs per-entry isolation + wrapper fallback** →
  classification by wrapper PRESENCE (authoritative, no fallback;
  malformed/empty wrapper rejects), single-object rule for top-level
  command/url with guidance, bare map iff ≥1 object value so
  `{"good":{…},"bad":42}` reports `bad` per-entry; classification
  strictly separated from entry validation (design D2).
- **P1 edit-as-JSON had no policy** → replace mode on the import
  endpoint: exactly one entry, transports replaced wholesale via the
  existing update path, name unchanged (renames in Form mode), target
  untouched on any reject/failed entry; per-mode drafts retained
  (design D6, settings-shell delta).
- **P1 io failures absent from outcomes** → `io_error` is a reported
  failure reason with continue-semantics: the per-entry report is the
  response, never a discarding 500 (design D4, spec scenarios incl.
  read-only-directory import).
- **P2 duplicate-vs-capacity precedence** → duplicate detection
  (snapshot + occupied-target stat + same-paste slug collisions) runs
  before capacity in the import loop (design D4).
- **P2 field policy not deterministic** → normative table in the
  mcp-import spec (types, absent/null, skip-vs-fail, OS alias
  normalization win32→windows, transport conflicts with fields
  winning, warnings name keys never values).
- **P2 caps/parser policies design-only** → 32-entry cap, 256 KiB byte
  basis, paste-order outcomes, complete-parse-through-EOF, BOM and
  trailing-content rejection are now spec scenarios; all document-level
  checks precede the first create.
- **P2 slug normalizer not supplied by create** → `registry.Slugify`
  exported as the one normalizer (algorithm unchanged); collision and
  no-reservation semantics specified (design D5).
- **P2 D6 rename sequencing** → mandatory rebase step recorded: after
  the predecessor archives, drop this change's RENAMED section and
  re-validate (design D7, tasks workflow follow-up).

Post-fold `openspec validate` green.

## Implementation review (task 4.1) — gpt-6.1-sol

Verdict: **BLOCK — 2 P1, 8 P2**; boundaries, raw-text parsing,
classification, duplicate-before-capacity, io-continuation, predecessor
protections, and structure confirmed correct. All findings fixed (one
coverage item dispositioned):

- **P1 JSON null bypassed field validation** — strict decoding
  (`decodeString`/`rawIsNull`): null command/url/args/env (whole and
  per-value) fail the entry; replace-mode rejection preserves the
  target's bytes AND snapshot (tested).
- **P1 empty `replaceName` silently created** — replace mode is selected
  by PRESENCE; an empty selector is a typed 400 with nothing created
  (tested).
- **P2 unconfigured registry as 200** — read-only `Registry.Ready(kind)`
  preflight: nil/unconfigured → document-level 500 `unconfigured`
  before any entry (tested); ordinary write failures stay per-entry.
- **P2 warnings echoed values** — coercion/type/os warnings name keys
  and kinds only; non-echo assertions cover numeric env, foreign type,
  and os values.
- **P2 nondeterministic skip policy** — skip controls run in fixed
  order (disabled outranks os) decoupled from map iteration; a
  present, non-empty selector with no recognized token now SKIPS per
  the spec (test updated to the specified behavior).
- **P2 warnings hidden on replace** — a clean replace closes the
  dialog; any warnings keep the report visible (Playwright-tested).
- **P2 mid-import close race** — `runImport` captures the initiating
  dialog, ALWAYS reconciles the list, and only mutates dialog state if
  still current (delayed-route test).
- **P2 undocumented 404** — contract-first: replace-target 404 added to
  the import endpoint, clients regenerated, zero drift.
- **P2 coverage gaps** — added: exact-256-KiB acceptance, invalid
  UTF-8, last-slot slug collision (skip beats capacity),
  failure-then-valid ordering, ambiguous-command wrapping guidance,
  UI 33-entry + trailing-content rejections, warnings-visible replace,
  mid-import close, and newline-args/empty-arg/multiline-env JSON
  round-trip. **Dispositioned:** "io_error after an earlier successful
  write" is not deterministically injectable without an FS seam
  (read-only dirs fail all entries; the success-then-capacity pair
  covers continuation) — continuation semantics are covered by the
  all-io read-only test plus the registry's own injected-failure suite.
- **P2 stray YAML residue** — 128 accidental `backend/internal/server/
  [a-z][a-z].yaml` files (from an empty-dirs test bug) removed; the
  test now uses remembered dirs.

## Final verification (task 4.2)

Boundaries clean (eino 0 outside the adapter; no internal→client
imports), zero codegen drift, contract lint clean, `go vet` OK,
golangci-lint 0 issues, `go test -race` 10/10 packages, `pnpm check`
0 errors, build clean, Playwright 48/48, no stray artifacts.
D7 predecessor-archive rebase remains recorded for archive time.

## Delta review (task 5.2, enablement + detail popup) — gpt-6.1-sol

Verdict: **BLOCK — 1 P1, 4 P2**; core enablement representation,
contract/handler agreement, disabled-stays-managed/probeable, and row
control legality confirmed correct. All findings fixed:

- **P1 form edits silently re-enabled** — Form update payloads carry
  `enabled: editing.enabled`; browser regression proves a disabled
  server survives edit+rename as disabled.
- **P2 toggle failures erased** — toggle reconciles (reload) then
  retains the outcome; failures render near the list AND inside the
  open popup (tested with a failing PATCH + successful GET).
- **P2 declared modality not implemented** — the popup now moves focus
  in (restoring to the opener on close), traps Tab within the card,
  and Escape closes ONLY the popup (preventDefault keeps the native
  Settings dialog from cancelling; keyboard-tested).
- **P2 spec deltas missing** — root cause found: earlier failed command
  chains silently dropped the api-contract enablement requirement, the
  settings-shell popup/toggle scenarios, and the handler round-trip
  test. All restored and validating.
- **P2 handler coverage absent** — TestMcpEnabledHandlerRoundTrip
  restored (default-enabled create, explicit-false create, list,
  probe-while-disabled via fake prober, rename carrying false, absent
  re-enables, imports enabled).

Final verification after fixes: boundaries clean, ZERO_DRIFT, lint 0,
race 10/10, frontend 0 errors, build clean, Playwright 51/51.
