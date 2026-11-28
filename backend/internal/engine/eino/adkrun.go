// The ADK run path (engine-adk-spine): a run executes as ONE
// adk.ChatModelAgent under adk.Runner; the engine's concerns are the
// middlewares (tool plane at agent construction, budget, persistence,
// usage) and this driver, which translates the runner's event stream
// into the durable-first timeline the legacy loop emits. Terminal states
// are decided here — the ADK calls no hook on error termination.

package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// runAgent is the ADK twin of runLoop: returns the final answer text
// ("" on failure/cancellation, after describing it on the stream).
func (a *Adapter) runAgent(ctx context.Context, events chan domain.TimelineEvent, in runInput) (string, error) {
	s, err := a.prepareRun(ctx, events, in)
	if err != nil {
		return "", err
	}
	seed, summary, err := a.seedMessages(in)
	if err != nil {
		s.emit(domain.EventAgentStep, "engine: "+err.Error())
		a.finishRun(ctx, s.events, in, "failed", err.Error(), 0, 0)
		return "", err
	}
	st := newRunState(in.maxSteps)
	offered, dropped, collided := a.inventory(ctx, events, in)
	if line := a.reportDrops(st, dropped, collided); line != "" {
		s.emit(domain.EventAgentStep, line)
	}
	base := baseModelOf(s.chatModel)
	// One offload directory per run (the continuation shares it): oversized
	// tool results live here for the run's duration only — the durable rows
	// keep the originals — and go with it.
	var offload *offloadRoot
	if a.opts.ToolResultCap > 0 && (in.toolAllow == nil || len(in.toolAllow) > 0) {
		if o, err := newOffloadRoot(); err == nil {
			offload = o
			defer func() {
				o.close()
				_ = os.RemoveAll(o.dir) //nolint:errcheck // best-effort cleanup of a temp dir
			}()
		} else {
			s.emit(domain.EventAgentStep, "engine: tool-result offload unavailable (results stay uncapped): "+err.Error())
		}
	}
	input := seed
	for attempt := 0; ; attempt++ {
		final, err := a.runOnce(ctx, s, in, base, input, summary, st, offered, offload)
		if err != nil {
			return "", err // the failure path already described + finished itself
		}
		if ctx.Err() != nil {
			// Cancelled while the model answered: never record that answer
			// as the run's success (the legacy loop's post-Generate check).
			return "", a.failRun(ctx, s, in, st, ctx.Err())
		}
		// The guard against narrated tool use (delegation-truthfulness):
		// an agent that declared require_tool_use and answered its FIRST
		// step without tools gets exactly one nudge. On the ADK a tool-free
		// completion IS the terminal state, so the nudge is a continuation
		// run seeded with [history, answer, nudge] under the same execution.
		if attempt == 0 && in.requireToolUse && st.outputs == 1 {
			// The nudged answer stays in the transcript as an intent row
			// (ToolCalls "[]") — deliberately NOT a final-answer row.
			a.appendAssistant(in, "nudge", final, "[]")
			s.emit(domain.EventAgentStep, "engine: "+in.agent()+" answered without using any tools — nudged once (require_tool_use)")
			input = append(append(append([]*schema.Message{}, seed...), final), schema.UserMessage(toolUseNudge))
			if !in.isChild {
				// A root's nudge rides ONE call (legacy parity); tracked by
				// identity — the user may type the same words.
				st.stripMsg = input[len(input)-1]
			}
			continue
		}
		a.appendAssistant(in, "final", final, "")
		s.emit(domain.EventAgentStep, final.Content)
		s.emit(domain.EventSession, "run completed")
		tokensIn, tokensOut := st.usage()
		a.finishRun(ctx, s.events, in, "completed", "", tokensIn, tokensOut)
		return final.Content, nil
	}
}

// runOnce drives one agent run end to end and returns the terminal
// (tool-free) assistant message, or an error after describing the
// failure on the stream and finishing the execution.
func (a *Adapter) runOnce(ctx context.Context, s *runSetup, in runInput, base model.BaseModel[*schema.Message], input []*schema.Message, summary string, st *runState, offered []tools.Tool, offload *offloadRoot) (*schema.Message, error) {
	// The continuation run inherits the REMAINING iteration budget (the
	// legacy ceiling counts every model call of the run, nudged or not).
	remaining := st.steps - st.calls
	if remaining <= 0 {
		return nil, a.failRun(ctx, s, in, st, adk.ErrExceedMaxIterations)
	}
	// Slice 1 — the context-budget middlewares (operator decisions D3/D4/D5)
	// and the engine-provided tools they need on the plane.
	// `tools: []` is an explicit no-tools inventory (slice 0's invariant):
	// no engine extras either — nothing to cap, nothing to load.
	noTools := in.toolAllow != nil && len(in.toolAllow) == 0
	var extras []tools.Tool
	var budgeting []adk.ChatModelAgentMiddleware
	if offload != nil && !noTools {
		extras = append(extras, readFileTool{offload: offload})
		exempt := []string{dispatchDelegateTool, skillToolName}
		for _, t := range offered {
			if strings.HasPrefix(t.Def().Name, "delegate_") {
				exempt = append(exempt, t.Def().Name)
			}
		}
		red, err := reduction.New(ctx, &reduction.Config{
			Backend:           offloadBackend{offload: offload},
			ReadFileToolName:  readFileToolName,
			RootDir:           offload.dir,
			MaxLengthForTrunc: a.opts.ToolResultCap,
			MaxTokensForClear: int64(a.summarizeAt()), // tool results alone never fill the window
			// The delegation fold-backs carry the deliverable receipts at
			// their END (a head-only preview would cut them); a loaded skill
			// body is exactly what the model asked for. Exempt from BOTH
			// phases: every delegation tool on the plane (the per-agent
			// names and the dispatch alias).
			TruncExcludeTools: exempt,
			ClearExcludeTools: exempt,
		})
		if err != nil {
			return nil, a.failRun(ctx, s, in, st, fmt.Errorf("reduction middleware: %w", err))
		}
		budgeting = append(budgeting, red)
	}
	budget := &budgetMW{st: st, emit: s.emit}
	persist := &persistenceMW{a: a, ctx: ctx, events: s.events, in: in, st: st, emit: s.emit, extras: extras}
	usage := &usageMW{st: st, effort: in.reasoningEffort, opt: a.effortOpt}
	// In-run compaction: the thin bridge over the kernel policy
	// (internal/compaction behind domain.Compactor — the constitution's
	// Compaction Rule), bound to the run's model for the summarizer.
	if a.compactorFor != nil {
		budgeting = append(budgeting, &compactionMW{
			a: a, ctx: ctx, events: s.events, in: in, emit: s.emit,
			compactor: a.compactorFor(in.providerName, in.modelID),
			budget:    domain.TokenBudget{MaxTotalTokens: a.summarizeAt()},
		})
	}
	// Hook order = composition order: reduction shrinks tool results, then
	// compaction counts what is left, the persistence and budget hooks see
	// the final state (the budget's nudge must stay the last message).
	handlers := append(budgeting, persist, budget)
	// Skills on demand only when the analysis RAN: a nil list means no
	// analysis (bypass/failure) — no skill tool, as the bodies path had no
	// skills section; a non-empty-or-empty-but-non-nil list offers the
	// recommendations (empty = every provisioned skill, decision D3).
	if a.opts.ProgressiveSkills && a.snapFn != nil && !noTools && in.skills != nil {
		name := skillToolName
		sk, err := skill.NewMiddleware(ctx, &skill.Config{Backend: &scopedSkillBackend{snap: a.snapFn, allow: in.skills}, SkillToolName: &name})
		if err != nil {
			return nil, a.failRun(ctx, s, in, st, fmt.Errorf("skill middleware: %w", err))
		}
		handlers = append(handlers, sk) // injects the `skill` tool at BeforeAgent (foreign to the plane: passed through)
	}
	handlers = append(handlers, usage)

	toolsForNode := einoTools(append(append([]tools.Tool{}, offered...), extras...))
	if len(toolsForNode) == 0 {
		toolsForNode = []tool.BaseTool{placeholderTool{}} // see placeholderTool
	}
	// The model-facing inventory is re-derived every call from the LIVE
	// registry/ward/MCP state (the legacy loop rebuilt its tool list per
	// step: a mid-run ward entry changes the delegate tool's description,
	// a newly probed MCP server adds tools, an empty plane advertises
	// nothing). The compiled node's tools are only name slots: every call
	// is executed against the live inventory (persistenceMW.execute), and
	// names that appeared since the node was compiled reach the same path
	// through the unknown-tool handler. Tool infos the middlewares add
	// themselves (the skill tool) are kept, not replaced.
	compiled := map[string]bool{}
	for _, t := range toolsForNode {
		if info, err := t.Info(ctx); err == nil {
			compiled[info.Name] = true
		}
	}
	budget.refresh = func(state *adk.ChatModelAgentState) {
		if st.suppress {
			return
		}
		// A live inventory change (a reserved-prefixed MCP tool probed
		// mid-run, a roster hot-reload) reports only what is newly dropped.
		if _, dropped, collided := a.inventory(ctx, s.events, in); len(dropped)+len(collided) > 0 {
			if line := a.reportDrops(st, dropped, collided); line != "" {
				s.emit(domain.EventAgentStep, line)
			}
		}
		if st.foreign == nil {
			// First call: whatever the middlewares added beyond the compiled
			// inventory (the skill tool) is theirs — kept, never re-derived.
			st.foreign = map[string]bool{}
			for _, info := range state.ToolInfos {
				if info != nil && !compiled[info.Name] {
					st.foreign[info.Name] = true
				}
			}
		}
		var infos []*schema.ToolInfo
		for _, t := range einoTools(persist.inventory()) {
			if info, err := t.Info(ctx); err == nil {
				infos = append(infos, info)
			}
		}
		for _, info := range state.ToolInfos {
			if info != nil && st.foreign[info.Name] {
				infos = append(infos, info)
			}
		}
		state.ToolInfos = infos
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          in.agent(),
		Description:   "Karta run",
		Instruction:   in.systemPrompt,
		Model:         base,
		GenModelInput: genModelInput(summary),
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               toolsForNode,
			ExecuteSequentially: true, // the legacy loop's one-at-a-time tool rounds
			UnknownToolsHandler: persist.unknownHandler(),
		}},
		MaxIterations: remaining, // the run's absolute ceiling, less the calls already made
		Handlers:      handlers,
	})
	if err != nil {
		return nil, a.failRun(ctx, s, in, st, err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	iter := runner.Run(ctx, input)
	var final *schema.Message
	for {
		evt, ok := iter.Next()
		if !ok {
			break
		}
		if evt.Err != nil {
			return nil, a.failRun(ctx, s, in, st, evt.Err)
		}
		if evt.Output == nil || evt.Output.MessageOutput == nil {
			continue
		}
		msg, err := evt.Output.MessageOutput.GetMessage()
		if err != nil {
			return nil, a.failRun(ctx, s, in, st, err)
		}
		if msg.Role != schema.Assistant {
			continue // tool results already surfaced through the wrapper
		}
		st.outputs++
		if len(msg.ToolCalls) == 0 {
			final = msg
		}
	}
	if final == nil {
		// No answer landed inside the iteration ceiling.
		return nil, a.failRun(ctx, s, in, st, adk.ErrExceedMaxIterations)
	}
	return final, nil
}

// failRun describes a failed/cancelled run on the stream and finishes
// the execution — the legacy loop's terminal texts, by cause.
func (a *Adapter) failRun(ctx context.Context, s *runSetup, in runInput, st *runState, err error) error {
	tokensIn, tokensOut := st.usage()
	var stuck errStuck
	switch {
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		// Cancelled while the model answered: never record that answer
		// as the run's success.
		s.emit(domain.EventSession, "run cancelled")
		a.finishRun(ctx, s.events, in, "cancelled", "", tokensIn, tokensOut)
		return fmt.Errorf("engine: cancelled: %w", err)
	case errors.As(err, &stuck):
		s.emit(domain.EventAgentStep, "engine: stopping — agent stuck (progress score "+fmt.Sprint(stuck.score)+")")
		a.finishRun(ctx, s.events, in, "failed", "stuck: no progress", tokensIn, tokensOut)
		return stuck
	case errors.Is(err, adk.ErrExceedMaxIterations):
		s.emit(domain.EventSession, "run stopped: step bound reached")
		a.finishRun(ctx, s.events, in, "failed", "step bound reached", tokensIn, tokensOut)
		return fmt.Errorf("engine: step bound reached: %w", err)
	default:
		s.emit(domain.EventAgentStep, "engine: model error: "+err.Error())
		a.finishRun(ctx, s.events, in, "failed", err.Error(), tokensIn, tokensOut)
		return fmt.Errorf("engine: model error: %w", err)
	}
}

// genModelInput composes the model's input exactly as the legacy loop
// did: the system prompt, the compaction summary under it, then the
// run's messages. Custom rather than the ADK default so prompts
// containing braces (JSON examples) are never template-formatted.
func genModelInput(summary string) adk.GenModelInput {
	return func(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
		msgs := make([]*schema.Message, 0, len(input.Messages)+2)
		if instruction != "" {
			msgs = append(msgs, schema.SystemMessage(instruction))
		}
		if summary != "" {
			msgs = append(msgs, schema.SystemMessage(summary))
		}
		return append(msgs, input.Messages...), nil
	}
}

// seedMessages is the agent's INPUT: a root run's compaction-aware
// history rows for its execution (the goal row included) plus the
// summary to ride under the system prompt; a child's FRESH context is
// the task alone (the parent embeds needed context in the task).
func (a *Adapter) seedMessages(in runInput) (seed []*schema.Message, summary string, err error) {
	if in.isChild {
		return []*schema.Message{schema.UserMessage(in.goal)}, "", nil
	}
	hw, err := a.st.RootWindowFull(in.sessionID, in.execID, 200)
	if err != nil {
		return nil, "", fmt.Errorf("engine: history: %w", err)
	}
	if hw.Summary != nil {
		summary = hw.Summary.Content
	}
	return rowsToMessages(hw.Rows), summary, nil
}

// rowsToMessages converts history rows (user/assistant/tool) into the
// model's message shapes — assistant intent rows keep their tool calls.
func rowsToMessages(rows []store.Message) []*schema.Message {
	return rowsToMessagesOverlay(rows, nil)
}

// rowsToMessagesOverlay is rowsToMessages with what the MODEL saw per
// tool-call OCCURRENCE overlaid (call id → contents in order): the legacy
// root re-reads durable rows, which keep the REAL outputs, so a PostToolUse
// block's replacement is applied here — by occurrence, since a model may
// reuse a call id within a run (an allowed result must never inherit an
// earlier block, nor the reverse).
func rowsToMessagesOverlay(rows []store.Message, modelFacing map[string][]string) []*schema.Message {
	seen := map[string]int{}
	msgs := make([]*schema.Message, 0, len(rows))
	for _, m := range rows {
		switch m.Role {
		case "user":
			msgs = append(msgs, schema.UserMessage(m.Content))
		case "assistant":
			am := &schema.Message{Role: schema.Assistant, Content: m.Content}
			if m.ToolCalls != "" {
				_ = json.Unmarshal([]byte(m.ToolCalls), &am.ToolCalls)
			}
			msgs = append(msgs, am)
		case "tool":
			content := m.Content
			if q := modelFacing[m.ToolCallID]; seen[m.ToolCallID] < len(q) {
				content = q[seen[m.ToolCallID]] // what the model saw for THIS occurrence
				seen[m.ToolCallID]++
			}
			msgs = append(msgs, &schema.Message{Role: schema.Tool, Content: content, ToolCallID: m.ToolCallID, ToolName: m.ToolName})
		}
	}
	return msgs
}

// summarizeAt is the in-run summarization trigger (0 = off).
func (a *Adapter) summarizeAt() int { return a.opts.SummarizeAt }

// emitCompaction persists, publishes and streams a compaction_triggered
// event with its stats (attempted-durable-first, the run's attribution).
func (a *Adapter) emitCompaction(ctx context.Context, events chan domain.TimelineEvent, in runInput, stats *domain.CompactionStats) {
	text := fmt.Sprintf("context summarized in-run (chars/4 estimate): %d → %d tokens (%d → %d messages)", stats.TokensBefore, stats.TokensAfter, stats.MessagesBefore, stats.MessagesAfter)
	chanKind, kind := domain.EventCompactionTriggered, domain.EventCompactionTriggered
	if in.isChild {
		chanKind = domain.EventDelegation
	}
	evt := domain.TimelineEvent{ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID, Kind: chanKind, Text: text, Agent: in.agent(), Compaction: stats}
	payload, _ := json.Marshal(map[string]any{"text": text, "agent": in.agent(), "compaction": stats})
	_ = a.st.AppendEvent(store.Event{ID: evt.ID, SessionID: in.sessionID, ExecutionID: in.execID, Kind: string(kind), Payload: string(payload)}) //nolint:errcheck // attempted-durable-first (F-15 posture)
	if a.bus != nil {
		_ = a.bus.Publish(context.Background(), evt) //nolint:errcheck // fire-and-forget emit (F-15) // ctx-ok: F-15
	}
	select {
	case events <- evt:
	case <-ctx.Done():
	}
}

// reportDrops formats the reserved-name / wire-name report for what has not
// been reported yet in this run ("" when nothing new); it remembers what it
// reported.
func (a *Adapter) reportDrops(st *runState, dropped []tools.Tool, collided []string) string {
	if st.reportedDrops == nil {
		st.reportedDrops = map[string]bool{}
	}
	var names []string
	for _, t := range dropped {
		if n := t.Def().Name; !st.reportedDrops["tool:"+n] {
			st.reportedDrops["tool:"+n] = true
			names = append(names, n)
		}
	}
	var agents []string
	for _, n := range collided {
		if !st.reportedDrops["agent:"+n] {
			st.reportedDrops["agent:"+n] = true
			agents = append(agents, n)
		}
	}
	var parts []string
	if len(names) > 0 {
		parts = append(parts, "engine: tools with the reserved "+reservedToolPrefix+" prefix are not offered: "+strings.Join(names, ", "))
	}
	if len(agents) > 0 {
		parts = append(parts, "engine: agents without their own delegation tool (name not verbatim-safe or colliding; reachable as "+dispatchDelegateTool+"): "+strings.Join(agents, ", "))
	}
	return strings.Join(parts, "\n")
}
