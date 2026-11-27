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

- **The ADK run path (engine-adk-spine, slice 0)** — `engine_loop: adk`
  (`Adapter.SetLoop`; default `legacy` until the delegation slice) runs a
  root as ONE `adk.ChatModelAgent` under `adk.Runner`; children still run
  the legacy loop via the delegate tool in this slice. `run.go` dispatches;
  both paths share `prepareRun` (`runsetup.go`: attribution, the
  durable-first `emit` closure, the model cache, the goal row) and
  `offeredTools` (`runhelpers.go`: the per-run inventory) and
  `budgetFor` (`runstate.go`).
  - `adkrun.go` is the driver: seeds the agent input from the store
    window (`seedMessages`/`rowsToMessages`; a child's is the task alone),
    composes the model input exactly like the legacy loop
    (`genModelInput`: system prompt, compaction summary, messages),
    consumes the runner's event stream, and decides every terminal state
    (`failRun`: cancelled / stuck / step bound / model error — the ADK
    calls no hook on error termination). The final and nudge rows are
    persisted HERE (only the driver knows a tool-free response is THE
    answer); intent rows in the persistence middleware.
  - Middlewares (each `adk.BaseChatModelAgentMiddleware` + the hooks it
    needs): `mw_budget.go` (tracker tick, extension events, the stuck kill
    as a typed `errStuck`, near-ceiling tool suppression via
    `state.ToolInfos`, the transient progress nudge — appended before the
    call, stripped after, never persisted); `mw_persistence.go` (intent
    row + `agent_step` after a tool-calling response; the tool-call
    wrapper: execute → `tracker.Record` → `emitToolCall`; refusals —
    hallucinated names via the ToolsNode's unknown handler, suppressed
    calls, tool-less runs — get the typed `ErrToolNotOffered` text as
    the result, are recorded and surface as rows/events exactly like
    executed calls); `mw_usage.go` (`WrapModel`: usage onto the run
    totals; slice 1 adds reasoning effort here).
  - `einotool.go`: `tools.Tool` → `InvokableTool` (an Exec error is the
    endpoint error; the wrapper turns it into the model-facing text). The
    ADK fails the run on a tool error — never return one from an adapter.
    `placeholderTool` keeps a TOOL-LESS run on the ReAct path: the ADK
    otherwise degrades to a single call with no tool node, so a
    hallucinated call would become the "answer"; the placeholder is never
    offered (ToolInfos emptied) and never executes (refused).
  - `require_tool_use` on the ADK: a tool-free completion IS the terminal
    state, so the one nudge is a CONTINUATION run seeded with
    [history, answer, nudge] under the same execution (`runAgent`); the
    nudged non-answer is persisted as the `msg-nudge-` row with
    `ToolCalls "[]"` exactly like the legacy loop.
  - Gate-driven refinements (terra, two passes): the driver checks
    `ctx.Err()` after a successful agent run before accepting an answer
    (a late answer after Cancel is never the run's success — legacy
    post-Generate parity) and the nudge branch never consults the context;
    the nudge continuation inherits the REMAINING iteration budget
    (`MaxIterations = steps - calls`); refusal rows pair with their own
    call IDs (`runState.pendingID`: a per-name queue — sequential
    execution makes the order deterministic); the LIVE tool plane decides
    per call, exactly like the legacy per-step inventory: the model-facing
    list is re-derived every call (`budgetMW.refresh`), and every call —
    compiled name or not — executes through `persistenceMW.execute` →
    `execOffered` against the current plane (a tool removed, replaced or
    added mid-run is refused / the replacement runs / runs; an empty plane
    advertises nothing and refuses everything); `delegateTool.Exec`
    re-reads the registry (`refreshed()`): a hot-reload during a long run
    cannot leave a dropped agent invocable; `emitToolCall` gives the
    durable row and the published event ONE id (the row used to mint its
    own); progress nudges fire only while tools are offered (legacy
    parity — `Nudge` flips the tracker's warning flags); a root's
    `require_tool_use` nudge rides ONE call (`runState.stripMsg`, removed
    by pointer identity — the state carries the instruction prefix, and a
    user may type the same words).
  - Tests: `TestContractSuiteADK` runs the shared conformance suite on
    this path (same fixture, `suiteFixture(t, LoopADK)`); `adkrun_test.go`
    pins the surviving loop behaviors (row/event order + usage +
    attribution, hallucinated-tool refusal, the nudge continuation, the
    transient nudge, the stuck kill at call 11, the step bound at the
    ceiling, the suppressed-call refusal; the gate cases: cancel during
    the model call never completes, a nudged run shares the ceiling,
    refusals pair with their own call IDs, the delegate tool reads the
    live registry, the inventory follows it, the nudge rides one call,
    execution follows the live plane (removed/replaced/added), every
    event is durable before publication — checked at bus delivery). Test
    fakes implement only `Generate`; `baseModelOf` widens them to
    `model.BaseModel`.
