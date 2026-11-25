# Tasks

## 1. Contract + domain + store

- [ ] 1.1 Contract: `TimelineEvent.execution_id`, `ChatMessage.agent` +
  `execution_id`, `SessionState.executions[]` (SessionExecution),
  `DelegationPayload` cost fields, kinds `delegation_started/completed`.
  `CONTRACT_CHANGE=1 task codegen`; commit. Verify: codegen zero drift.
- [ ] 1.2 Domain kinds/payload; store: `messages.agent` migration +
  `Message.Agent`, `ExecutionUsage`, `SessionExecutions`. Verify:
  `TestMigrateMessagesAgent`, `TestExecutionUsage`,
  `TestSessionExecutions`.

## 2. Engine

- [ ] 2.1 Adapter: `agent` on every durable payload and the bus event
  (root → "root"), `Agent` on assistant rows, `reasoning_content`
  persisted; delegate tool emits `delegation_started/completed` with
  usage read back. Verify: `TestEngineRowsAndEventsAttributed`,
  `TestEnginePersistsReasoning`, `TestDelegationLifecycleEvents`.

## 3. Server

- [ ] 3.1 SSE frames carry `agent` + `execution_id` (live + replay);
  replay attributes legacy rows via the executions join; `chatView`
  carries agent/execution; `GetSessionState` lists executions. Verify:
  `TestSSEFramesAttributed` (live + legacy replay), `TestSessionStateExecutions`.

## 4. Frontend

- [ ] 4.1 Timeline store keeps `agent`/`executionId`; Work tab labels
  steps by agent, shows delegation cards (live from events) and the run
  footer (from state). Verify: `pnpm check`; Playwright "work tab labels
  agents and totals the run".
- [ ] 4.2 Thread: report cards vs answer; the answer block's Thinking
  (final row reasoning) + Narration. Verify: Playwright "reports never
  pose as answers" + "thinking is reasoning". Phase gate (gpt-6.1-sol);
  fix P0/P1.

## 5. Review and hardening

- [ ] 5.1 Phase gate (gpt-6.1-terra) over groups 1–3; fix P0/P1; P2s
  fixed or recorded in findings.md.
- [ ] 5.2 zbot-reviewer + `task verify` green (dev stack stopped).
