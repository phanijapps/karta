# engine/eino

The CloudWeGo Eino adapter — the ONLY package that imports
eino/eino-ext (constitution: engine isolation). A bounded loop over
Generate + WithTools with direct control of step counting, cancellation
checks, and durable-first event emission.

- `Adapter` implements `domain.AgentEngine` (RunGoal/Cancel; Resume is
  declared not-yet — the contract suite pins that deferral).
- `newModel` is the unexported model-factory seam: tests inject fake
  generators (no live provider); production builds the OpenAI-compatible
  client. The models cache keys on provider/model/token-hash.
- Known, pinned bugs (characterization tests in adapter_test.go flip
  when the run-ownership follow-up lands): caller ctx is detached after
  RunGoal returns (F-23); same-session overlap orphans the first run's
  cancel handle (F-24); the models-cache key omits BaseURL (F-26).
- Imports engine + domain + providers + store + tools + eventbus;
  nothing else may import eino.

- `Adapter.DropWard(sessionID)` resets the session's ward host on
  leave-ward so the next run starts unhosted (wired via the server's
  `SetWardDropper`). Tool-call bus events carry `Text` = tool name
  (every SSE consumer renders it).

- Ward events (add-ward-hosting): the per-session host's transition
  observer → `emitWardEvent` persists `ward_entered`/`ward_created`
  with the `WardEventPayload` then publishes (the F-15 posture).
  `delegateTool` composes the `## Active Ward` section onto the
  child's prompt and appends the ward guard to its description while
  hosted.
- System-role agents (intent-analyst-agent S1): `registry.IsSystemAgent`
  names host-run steps (the intent analyst). `delegateDescription` omits
  them from the routing table and `delegateTool.Exec` refuses them typed
  (`ErrNotDelegatable`) whatever their description — they are never
  workers, even though they sit in the roster for Settings.
- Attribution (run-legibility): every durable payload carries `agent`
  and the bus event carries `Agent` + `ExecutionID` (`runInput.agent()`
  — "root", a named root agent, or the child's name; provider/model is
  provenance, not an agent). Assistant rows carry `Agent` and the
  model's `Reasoning`; the root execution's agent column is set at run
  start (`SetExecutionAgent`). A delegation is a Timeline node:
  `emitDelegation` persists + publishes `delegation_started` (agent,
  task, execution) before the child's steps and `delegation_completed`
  (usage read back via `store.ExecutionUsage`, duration, stored status)
  after them. Ward transitions are attributed through the per-session
  `runAttr` map (`setRunAttribution` at every run's start). `run.go`
  is the root wrapper around `runLoop`.
- Delegation truthfulness (operator decisions 2026-11-25): children
  share the ROOT's tool plane — builtins, memory, ward file/shell and
  the live MCP tools — bounded only by the agent's own `tools:`
  allowlist (`adapter.go`, the MCP guard no longer excludes children).
  `require_tool_use: true` on an agent (registry `AgentSpec`) makes the
  run loop nudge ONCE when the FIRST completion carries no tool calls
  (`toolUseNudge`, `runhelpers.go`): the answer stays in the
  conversation with the nudge as a user message; the next answer is
  final. The delegate fold-back appends host-verified deliverable
  receipts for the paths the task named (`deliverableReceipts`). The
  depth cap is `SetDepthCap` (`delegation.max_depth`, default 1 — the
  root delegates, children never; nesting stays off until raised).
  Assistant rows carry per-step usage (`usageOrNull`). `runhelpers.go`
  holds the loop's helpers (model cache key, offered-inventory executor,
  nudge text, usage, `emitToolCall`).

