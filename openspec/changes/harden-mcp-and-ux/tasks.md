# Tasks

## 1. MCP hardening

- [x] 1.1 Typed failure taxonomy (ExecFailure: timeout / unavailable /
  protocol / server_error / bad_arguments) riding tool results;
  per-call bound typed. Verify: TestExecutorHardening (typed
  failures).
- [x] 1.2 Binary resolution: PATH then HOME tool dirs (node/pnpm/uv/
  cargo/go/bun/rye); absolute passthrough; verbatim fallback. Verify:
  TestExecutorHardening/resolveBinary. LIVE: the operator's bare
  `command: npx` config (which failed before) now connects (9 tools).
- [x] 1.3 Restart backoff: consecutive start failures → 2s·2^n cap
  2min; Refresh honors the window BEFORE dropping a dead entry (an
  ordering bug the test caught: the drop-then-check order re-dialed
  every refresh). Verify: TestExecutorHardening/backoff.

## 2. /v1 streaming

- [x] 2.1 Contract: OpenAiCompletionChunk schema; stream=true docs;
  the 200 oneOf so codegen materializes the chunk type. Verify:
  codegen zero drift.
- [x] 2.2 openAiStream: chat deltas 1:1; research = Timeline
  subscribe → prefixed work frames → answer → stop; `[DONE]` as the
  writer's TERMINAL frame (a direct write raced the drain goroutine
  and printed [DONE] first — live-caught, test-pinned ORDER);
  finish_reason stop/error; busy 409 pre-stream. Verify:
  TestOpenAiStreaming (framing, bare data lines, order). LIVE: chat
  `content → stop → [DONE]`; research streamed 🔧/🧠 frames + the
  sourced AMD figure.
- [x] 2.3 tool_call bus events carry Text=tool name (every SSE
  consumer renders it — the work frames showed empty labels).

## 3. Leave ward

- [x] 3.1 Contract POST /api/sessions/{id}/leave-ward (204 / 404 /
  409 default); store unbind; Host.Leave + adapter DropWard
  (SetWardDropper); handler idempotent. Verify: TestLeaveWardHandler.
- [x] 3.2 UI: the composer's ward strip gains Leave (non-warden
  sessions); wardens note "the warden always lives here"; roster
  refresh after. Verify: check/build; the UX choice is recorded.

## 4. Research auto-titling

- [x] 4.1 Wrapper TitleSession hook from the first analysis's
  primary_intent (SlugTitle: ASCII words, ≤64); store
  SetSessionTitleIfUntitled never overwrites. Verify:
  TestAutoTitleFromAnalysis, TestAutoTitleIfUntitled.

## 5. Review and hardening

- [x] 5.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol); fix P0/P1, P2s
  fix or record. Verify: ledger zero open P0/P1.
- [x] 5.2 zbot-reviewer + `task verify` green. Verify: reviewer zero
  open P0/P1; verify exits zero.
