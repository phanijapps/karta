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

- **The context budget (engine-adk-spine slice 1)** — `RunOptions`
  (`SetRunOptions`, applied by the daemon from config) switches on Eino's
  reduction middleware, the kernel compaction bridge and the effort seam;
  the glue lives in `contextbudget.go`, the wiring in `runOnce`
  (`adkrun.go`). Hook order is composition order: reduction → compaction
  → persistence → budget → skill → usage (reduction shrinks tool results
  before the policy counts them; the budget's nudge stays the last
  message; the persistence row sees the ORIGINAL tool result before the
  model-facing copy is capped — the durable rows keep the original).
  - `reduction` (cap in BYTES, `compaction.tool_result_cap`, default 12 KB):
    an oversized tool result becomes a `<persisted-output>` notice with the
    first/last ~cap/4 bytes and a path; the ORIGINAL bytes are written by
    `offloadBackend` under a per-run `offloadRoot` (a temp dir created
    CANONICAL and held as an `os.Root`, whose methods can only reach
    files BENEATH the root — escapes, symlinked directories out of the
    tree and traversal writes are refused by the kernel-bounded root, no
    path-name races; removed with the run). The pointer is actionable
    through `karta_read_file` (`readFileTool`, an engine-provided extra:
    BYTE ranges — the solar dumps were single 25–38 KB lines — always
    advancing by a rune, opened through the same root).
    `delegate_agent` and `karta_skill` results are exempt from BOTH
    reduction phases (receipts sit at the fold-back's END; a loaded body
    is what the model asked for).
  - `skill` (progressive injection, `skills.injection: progressive`, ADK
    path only): the `karta_skill` tool offers exactly the analysis's
    recommended, provisioned skills (`GoalRequest.Skills`, set by the
    intent wrapper when the analysis ran; a non-nil empty list = every
    provisioned skill — decision D3; nil = no analysis → no skill tool).
    The middleware injects the tool at `BeforeAgent`, so it is FOREIGN to
    the user's plane: the budget's `refresh` captures such names once
    (`runState.foreign`) and keeps their tool infos, and
    `persistenceMW.execute` runs them through their own endpoint (an
    out-of-scope skill is a typed tool error the run survives, surfaced
    as a tool_call row/event). `tools: []` (an explicit no-tools
    inventory) gets neither `karta_skill` nor `karta_read_file` — the
    slice-0 invariant holds for engine extras. The `karta_` prefix is
    RESERVED for engine tools and ENFORCED at the tool-plane boundary
    (`withoutReserved`; the drop is reported once on the stream).
  - In-run compaction is the constitution's thin bridge (`mw_compaction.go`),
    NOT Eino's summarization middleware (gate G4 F1): the policy is
    `internal/compaction` behind `domain.Compactor`, bound per run by
    `SetCompactorFactory` (`compactorFor(provider, model)`); the bridge
    converts state to `domain.Message` (tool calls rendered into the
    transcript, token estimates attached), asks `ShouldCompact` against
    `RunOptions.SummarizeAt`, and on a pass keeps the instruction, inserts
    the ONE summary (previous summaries fold), and keeps the structured
    tail verbatim — then emits one durable-first `compaction_triggered`
    event with `CompactionStats`. A skipped/blocked pass is an
    `agent_step` line, never a run failure. Which model summarizes is the
    policy's business (`compaction.summarizer` overrides the run's model);
    PreCompact hooks are honored by the policy but the daemon has no
    `HookRunner` yet (G5 R2-1, recorded).
  - Reasoning effort (D5): `reasoning_effort` on the agent spec rides
    every ROOT model call of the run as the OpenAI-compatible
    `reasoning_effort` option (`withEffort`, appended in `WrapModel` —
    the one seam where call options are composed). The option is the
    adapter's `effortOpt` field (`DefaultEffortOption`); tests substitute
    a recorder on their own adapter. The summarizer's effort is the
    policy's business (not forced — D5's `none` waits for the per-model
    verification, task 2.4).
  - Tests (`contextbudget_test.go`): cap + BYTE-range re-read of the
    exact original + offload cleanup, reader/writer confinement (escapes,
    symlinks, traversal, a canonical root), reserved names, skill on
    demand (scoped, fallback, out-of-scope typed error, observable; none
    without analysis or with `tools: []`), the compaction bridge (event +
    stats, structured tail intact, a blocked hook), effort per agent,
    `clampRange` edges.

- **Slice 2 — delegation on the spine (decision D1)**: on the ADK loop
  `inventory()` offers ONE task tool per delegatable agent —
  `delegate_<agent>` with `{task}` (`perAgentDelegationTools`,
  `delegateToolName`: the `delegate_` prefix is what the UI recognizes as
  a delegation; an agent literally named `agent` would collide with the
  dispatch name and is not offered as its own tool). The legacy loop keeps
  its single `delegate_agent` dispatch tool. On the spine the dispatch
  name stays CALLABLE as an alias for prompts that still use it:
  `persistenceMW.execute` routes it to `dispatchDelegate` when the run
  may delegate. Both shapes share `delegateTool.Exec` — the validation
  (unknown / system / undescribed agents typed), the child execution
  record, `delegation_started`/`completed`, deliverable receipts and the
  parent-attribution restore are unchanged.
  - `runChild` runs the delegated child on the engine's run path: the
    spine when it is the loop (`runAgent` — children get the caps, the
    skill tool, in-run compaction and their `reasoning_effort`: tasks
    2.3b), the legacy loop otherwise. Children are separate agent runs
    driven by their own driver loop inside the tool call (NOT
    `adk.NewAgentTool`, which would fold child events into the parent's
    stream and bypass the driver's terminal handling); their steps
    surface in the parent's stream as delegation-kind events through the
    same emit closure, their rows persist under the child's execution.
  - `engine_loop` defaults to `adk` (config + `New`); the legacy path is
    exercised explicitly by `TestContractSuite` (`suiteFixture(t,
    LoopLegacy)`) and by the legacy-only dispatch test; every other
    adapter test now runs on the spine.

  - Wire-name collisions (gate G7 F1): two agent names can map to one
    `delegate_<…>` name (`a.b` and `a_b`), and "agent" maps to the
    dispatch name — such agents get NO tool of their own; `inventory()`
    returns them as `collided`, the driver reports them once on the
    stream, and they stay reachable through the dispatch alias. Every
    delegation tool on the plane — per-agent names and the alias — is
    exempt from both reduction phases (the receipts sit at the fold-back's
    END). A hallucinated `delegate_<unknown>` is refused naming the
    delegation tools that ARE offered.
