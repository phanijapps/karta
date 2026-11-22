# Tasks

## 1. Contract & codegen (contract-first)

- [x] 1.1 Add `POST /api/mcps/import` to `contracts/openapi.yaml`: request `{ document: string, name?, replaceName? }` (raw paste; the daemon is the only parser), the per-entry outcome schema (`source_key`, `name?`, `status` created|skipped|failed, `reason` incl. `io_error`, `warnings[]`), skip reasons (duplicate, disabled, other_os), failure reasons (shape, field, name, capacity, io_error), typed 400s for every document-level rejection (malformed JSON, duplicate keys, trailing content, BOM/non-UTF-8, non-object, unclassifiable, no servers, missing name, >256 KiB, >32 entries), the replace-mode contract (exactly one entry, name unchanged, target untouched on any reject/report-failure), and 200 outcomes in paste order; verify `npx @redocly/cli lint contracts/openapi.yaml` passes
- [x] 1.2 Regenerate Go server types and the TS client via codegen; verify a second run produces zero diff and `task check:frontend` passes

## 2. Parser, normalizer, endpoint

- [x] 2.1 Export `Slugify` from `internal/registry` (today's unexported algorithm, unchanged; doc comment naming it the one normalizer) with reserved-name, overlength, traversal-looking, Unicode-only, and separator-collapsing tests; verify registry tests stay green and `golangci-lint run` passes
- [x] 2.2 Build the raw-text parser and classifier in `internal/server`: order-preserving token walk (`json.Number`, duplicate-key detection incl. Unicode-escaped equivalents, complete-parse-through-EOF, BOM/non-UTF-8 rejection), the presence-authoritative ladder with the ambiguous-single-object rule and its guidance message, classification strictly separated from entry validation; verify table-driven tests cover every document-level rejection creating nothing, integer-key paste order, exact-boundary 32/33-entry and 256-KiB cases, and the `{"good":{…},"bad":42}` / `{"command":{…}}` / `{"mcpServers":[]}` / `{"servers":{…},"mcpServers":{}}` adversarial shapes
- [x] 2.3 Implement the per-entry mapper and loop: the normative field-policy table (warn-drop naming keys never values, sse deprecation, disabled/os skip with alias normalization win32→windows etc., fields beating transport hints, exact-text scalar env coercion), duplicate-before-capacity precedence (snapshot + occupied-target stat + same-paste slug collisions, failed entries reserving nothing), `io_error` continue-semantics preserving the per-entry report, and the replace mode (exactly one entry → full-replace via the existing update path, name unchanged, target untouched on any reject or failed entry); verify table-driven tests cover every field-policy row plus the full-registry duplicate-skip, same-paste slug-collision-after-last-slot, and read-only-directory mid-import (all entries report io_error, response stays 200-with-report, nothing created, disk truth) cases
- [x] 2.4 Implement the import handler (document-level 400s, 200 with ordered outcomes, unconfigured-registry 500 before any entry) and add guard regression tests (foreign Host and cross-origin mutations rejected on the import route); verify handler tests cover mixed-paste 200, every 400, replace-mode multi-entry rejection, and outcome order matching paste order
- [x] 2.5 Update `backend/internal/server/AGENTS.md` and the registry AGENTS.md note for `Slugify`; verify `go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass

## 3. Frontend dialog

- [x] 3.1 Add the typed import method to `shared/api/client.ts` (operation-checked bindings, per-entry outcome types, replace mode); verify `pnpm check` passes with no hand-rolled fetches outside the client module
- [x] 3.2 Rework MCP add/edit into a dialog: backdrop + card, Form | Paste JSON segmented control, Local/Remote/Both transport selector with visibility swapped by selection (URL never shown for Local-only), lossless per-line args and verbatim env editors preserved, per-mode drafts retained while the dialog is open; verify the add-via-form delta scenario is demonstrable (no URL field in Local mode)
- [x] 3.3 Wire Paste JSON: mono textarea (placeholder = the quickstart block), name field enabled only when the client's advisory pre-classification sees a single-object paste, import button, per-entry outcome rows (status color, reason, warnings) with the list refreshing behind the open dialog; edit prefills the server's standard block and saves through replace mode (multi-entry documents rejected client-visibly with the daemon's message); verify the paste and edit-round-trip delta scenarios are demonstrable against the dev daemon
- [x] 3.4 Extend Playwright flows: form dialog field visibility per transport selection, official-block paste with mixed outcomes (created/skipped/failed rendered in order), duplicate skip, malformed paste and trailing-garbage errors, 33-entry rejection, single-object paste requiring the name, edit-as-JSON round-trip preserving newline-bearing args and multiline env byte-exactly; verify `pnpm test` passes without a live daemon
- [x] 3.5 Update `frontend/README.md` (dialog modes, raw-paste import, tolerance and replace behavior); verify the documented commands run as written

## 4. Detail popup and enablement

- [x] 4.1 Add `enabled` to `contracts/openapi.yaml` (Mcp responses required, McpCreate/McpUpdate optional inputs, absent = enabled); regenerate with zero drift
- [x] 4.2 Registry: manifests carry enablement (absent = enabled; `enabled: false` persists through create/update/rename and reloads under strict decode); verify loader round-trip tests (default-true, explicit-false, restart-persist)
- [x] 4.3 Handlers map `enabled` in/out (edit full-replace sets it; the probe stays available on disabled servers); verify handler round-trip tests
- [x] 4.4 Frontend: selecting a server opens a detail popup (transports, path, enable switch, Test/Edit/Delete; probe results inside); per-row toggle switch PATCHing the full manifest with the flipped state, disabled rows dimmed and marked; verify Playwright flows (popup opens, toggle sends enabled:false with transports, disabled marking, probe still offered)

## 5. Review and hardening

- [x] 5.1 First reviewer round: the read-only `zbot-reviewer` subagent ran over the change; every P0/P1 finding fixed, P2s fixed or dispositioned (see evidence.md)
- [x] 5.2 Re-run `task verify` green AND the reviewer over the enablement/popup delta; fix every P0/P1 finding

## Workflow follow-up

- Archive only after `manage-mcps-and-skills` has archived AND the D7 rebase is done: drop this change's RENAMED section, keep the MODIFIED requirement under the new name, re-validate against the updated main spec, then archive after the review task is complete and `task verify` is green.
