# Tasks

## 1. Contract & codegen (contract-first)

- [x] 1.1 Add chat endpoints and schemas to `contracts/openapi.yaml` — `POST /api/chat/messages` (text + qualified model → user/assistant messages; 400 `model_required`, 502 typed provider failures) and `GET /api/chat/messages` (seq-ordered rows, `after_seq` page cursor where `next_cursor` is the last delivered seq — never the database tail, bounded pages, `truncated`) and `POST` returning every exchange-appended row plus its end cursor (failures return the persisted user row and cursor); `ChatMessage` schema: id, seq, role, content, created_at, tool_calls, tool_call_id, tool_name, provider, model, nullable request-usage prompt/completion tokens; verify `npx @redocly/cli lint contracts/openapi.yaml` passes
- [x] 1.2 Regenerate Go server types and the TS client via `task codegen`; verify a second run produces zero diff and `task check:frontend` passes with the new types

## 2. Store: SQLite messages (agentzero-inspired)

- [x] 2.1 Add the modernc.org/sqlite dependency and create `internal/store`: open `~/.zbot/data/zbot.db` (WAL), bootstrap the five-table schema from design D1 with schema-version discipline (sessions, executions tree with delegation/error/token rollups, append-only messages with execution linkage, tool fields, provenance, request usage, compactions scoped to a context view, timeline_events), `EnsureSession` (idempotent session row + root execution), `Append` with atomic per-session `seq` assigned inside the INSERT, cursor-paginated `Replay`, the compaction-aware exchange-complete `HistoryWindow` (latest in-scope marker mapped to system + suffix excluding summary rows, front-trimmed at exchange boundaries, `needsCompaction` signal when untrimmed suffix exceeds the bound), and durable `AppendEvent`/`Events` replay; add the package `AGENTS.md`; verify unit tests cover append/replay order, cursor filtering, execution linkage and provenance on every row, HistoryWindow with/without a marker, cutoff-inside-a-tool-group, marker-after-range, and large-suffix cases, plus FILE-BACKED WAL concurrency tests (busy handling, seq uniqueness — race detector alone doesn't prove SQLite locking) and restart-by-reopen persistence
- [x] 2.2 Verify the eternal-session semantics at the store level: the fixed `default` session needs no creation step (rows self-establish it) and replay of a never-used session is empty; verify with tests

## 3. Tools

- [x] 3.1 Create `internal/tools`: the `Tool` seam (name, description, parameters schema, exec) and the two builtins `clock.now` and `echo` (bounded output); add the package `AGENTS.md`; verify unit tests cover exec results, JSON-arg validation, and unknown-arg tolerance

## 4. Providers: chat completion

- [x] 4.1 Add a non-streaming `ChatCompletion` to `internal/providers` (OpenAI-compatible `POST {base_url}/chat/completions`, resolved-credential token, tool definitions/results pass-through, typed failures reusing existing reasons, request/response bounded); give the chat call its own generous configurable deadline (default 180s; discovery keeps 10s — the eino spike proved local 32B models exceed 60s); verify httptest-based tests cover plain completion, tool-call responses, auth/unreachable/malformed failures, a slow completion inside the chat deadline succeeding while exceeding the discovery bound would not, and that no credential value appears in errors

## 5. Chat service

- [x] 5.1 Create `internal/chat`: `Send` implementing the fixed flow (persist user row → bounded history window → completion → tool rounds capped at 2 with tool rows and per-round events → final assistant row), a `ChatCompleter` interface seam so tests inject scripted completions, and typed no-model refusal; add the package `AGENTS.md`; send serialized per session by a service mutex (no db transaction spans a provider call; tool intent row persisted before execution; failure keeps user + committed intermediate rows with no final row); verify unit tests cover the plain round trip, one tool round, the round cap, failure-after-tool-round preserving committed rows, a two-client serialization test proving exchanges never interleave, needsCompaction surfacing, and event emission (session events per append + tool_call events)
- [x] 5.2 Verify eventbus wiring end-to-end at the service level: a send with one tool round produces the exact expected event sequence for a test subscriber

## 6. Daemon surface

- [x] 6.1 Implement `POST /api/chat/messages` and `GET /api/chat/messages` handlers in `internal/server` (model split/validation, typed 400/502 mapping, cursor pagination, cross-origin guard applies) in a new `chat_handlers.go`; verify handler tests (httptest + fake completer + in-memory store) cover every api-contract delta scenario including model_required and provider-failure transcript retention
- [x] 6.2 Wire store + chat service + eventbus in `cmd/zbotd` (open db at startup, close on shutdown); verify a smoke run on a throwaway home: send with curl → replay → restart daemon → replay unchanged; record output in the change notes
- [x] 6.3 Update `backend/internal/server/AGENTS.md` and the `cmd/zbotd` note; verify `cd backend && go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass

## 7. Frontend

- [x] 7.1 Add `sendChatMessage` and `syncChatMessages` to `shared/api/client.ts` (typed by the generated schema; typed failures incl. `model_required`); verify `pnpm check` passes from `frontend/`
- [x] 7.2 Rework Chat.svelte + the session entity: transcript loads by paging to `truncated=false`, sends use the picker's qualified model with a pending state (no optimistic fakes) and reconcile from the last contiguously delivered cursor after EVERY outcome (merging by stable id, surfacing failure-persisted user rows), tool rows render as compact tool lines, daemon-down shows an explicit state, and the mock reply engine, sample conversations, search-over-samples, and New Chat action retire; the picker drops its "Simulated" badge; verify the simple-chat delta scenarios are demonstrable in the running UI
- [x] 7.3 Update Playwright tests with route mocks: multi-page transcript catch-up, send round trip incl. pending state and full-exchange rows, post-failure reconciliation showing the persisted user row, tool row rendering, model_required guidance, daemon-down state; retire fixture-chat assertions; verify `pnpm test` passes from `frontend/` without a live daemon
- [x] 7.4 Update `frontend/README.md` for the live chat (session persistence, no streaming yet); verify documented commands run as written

## 8. End-to-end verification & docs

- [x] 8.1 Manually verify against the local ollama on a throwaway home: pick a model, send messages, watch a tool round (ask for the time), restart the daemon and confirm the transcript survives with provider/model provenance on assistant rows, `curl` the sync endpoint paging with cursors to the end, confirm durable events replay, and confirm the real home's data dir is untouched; record the command log and screenshot in the change evidence note
- [x] 8.2 Update `docs/product/product-context.md`, `docs/architecture/frontend-shell.md`, and the root README: chat is live and persistent (one eternal session), replies from the selected model, basic tools, streaming still pending; verify docs claim only what task 8.1 demonstrated
- [x] 8.3 Confirm constitution invariants and run `task verify` green end-to-end

## 9. Review and hardening

- [x] 9.1 Run the read-only `zbot-reviewer` subagent over the change scope, change artifacts, and the five constitution boundaries; fix every P0/P1 finding and fix P2s or record in the change summary why not; verify findings are resolved with targeted tests
- [x] 9.2 Re-run `task verify` after the final review fixes and documentation edits; verify it is green with no generated drift or unrelated files before declaring implementation complete

## Workflow follow-up

- Apply after `add-model-catalog` is archived (picker selection feeds the chat request).
- Archive only after the review task is complete: reviewer ran, all P0/P1 findings fixed, and `task verify` green after the final edit.
