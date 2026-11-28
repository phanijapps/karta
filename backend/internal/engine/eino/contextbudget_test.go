package eino

// The context-budget middlewares (engine-adk-spine slice 1): tool-result
// caps with a ranged re-read, skills on demand scoped to the analysis,
// in-run summarization on the timeline, and the reasoning effort riding
// every call. Fakes throughout — no live provider.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/compaction"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// newBudgetAdapter builds an ADK-path adapter with a per-model fake
// factory (the run model and a separate summarizer, by model id).
func newBudgetAdapter(t *testing.T, newModel func(modelID string) chatGenerator, snapFn func() registry.Snapshot) *Adapter {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/budget.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := New(fakeProviderStore{}, db, eventbus.New(), tools.BuiltinProvider{}, snapFn)
	a.newModel = func(_ context.Context, _ providers.Provider, _, modelID string) (chatGenerator, error) {
		return newModel(modelID), nil
	}
	if err := a.SetLoop(LoopADK); err != nil {
		t.Fatal(err)
	}
	return a
}

// stepGen scripts completions with a function of (call index, the call's
// messages) — for flows that must read a tool result to continue.
type stepGen struct {
	mu    sync.Mutex
	calls [][]*schema.Message
	opts  [][]model.Option
	next  func(call int, msgs []*schema.Message) *schema.Message
}

func (g *stepGen) Generate(_ context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	g.mu.Lock()
	call := len(g.calls)
	g.calls = append(g.calls, append([]*schema.Message(nil), msgs...))
	g.opts = append(g.opts, opts)
	g.mu.Unlock()
	out := g.next(call, msgs)
	if out.Role == "" {
		out.Role = schema.Assistant
	}
	return out, nil
}

func toolCallMsg(name, args string) *schema.Message {
	msg := &schema.Message{Role: schema.Assistant}
	msg.ToolCalls = []schema.ToolCall{{ID: "call-" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}
	return msg
}

// lastToolResult returns the last tool message of a call's input.
func lastToolResult(msgs []*schema.Message) *schema.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.Tool {
			return msgs[i]
		}
	}
	return nil
}

var savedToPattern = regexp.MustCompile(`saved to: (\S+)`)

// TestADKToolResultCapOffloadsAndRereads: a 30 KB single-line dump (the
// solar run's shape) is capped to a head + tail preview with a pointer;
// the model re-reads a BYTE range with karta_read_file — the exact
// original bytes; the durable row keeps the whole dump; the offload goes
// away with the run.
func TestADKToolResultCapOffloadsAndRereads(t *testing.T) {
	t.Parallel()
	const dump = "0123456789"
	big := namedTool{name: "big", out: strings.Repeat(dump, 3000)} // 30,000 bytes, ONE line
	gen := &stepGen{}
	var savedTo string
	gen.next = func(call int, msgs []*schema.Message) *schema.Message {
		switch call {
		case 0:
			return toolCallMsg("big", `{}`)
		case 1:
			res := lastToolResult(msgs)
			if res == nil || !strings.Contains(res.Content, "<persisted-output>") || len(res.Content) > 6000 {
				t.Fatalf("call 1 must see the capped notice (head + tail previews), not the 30 KB dump: %d bytes", len(res.Content))
			}
			m := savedToPattern.FindStringSubmatch(res.Content)
			if m == nil {
				t.Fatalf("the notice must point at the offload: %.300q", res.Content)
			}
			savedTo = m[1]
			return toolCallMsg(readFileToolName, `{"path":`+quote(savedTo)+`,"offset":10000,"limit":30}`)
		default:
			res := lastToolResult(msgs)
			if res == nil || !strings.HasPrefix(res.Content, "[bytes 10000-10030 of 30000] — continue with offset=10030") || !strings.HasSuffix(res.Content, "\n"+dump+dump+dump+"\n") {
				t.Fatalf("karta_read_file must return exactly bytes 10000-10030 of the original: %q", res.Content)
			}
			return &schema.Message{Content: "done"}
		}
	}
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, nil)
	a.toolProv = &liveProvider{ts: []tools.Tool{big}}
	a.SetRunOptions(RunOptions{ToolResultCap: 4096})
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-cap", Goal: "adk-cap goal", Model: "prov/m1"}))
	msgs, _ := a.st.Replay("adk-cap", 0, 50)
	var full int
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolName == "big" && len(m.Content) == 30000 {
			full++
		}
	}
	if full != 1 {
		t.Fatalf("the durable tool row keeps the full dump: %d rows", full)
	}
	if savedTo == "" {
		t.Fatal("no offload path observed")
	}
	if _, err := os.Stat(savedTo); !os.IsNotExist(err) {
		t.Fatalf("the offload must go away with the run: %v", err)
	}
}

func quote(s string) string { return `"` + s + `"` }

// TestADKSkillToolLoadsOnDemand: with progressive skills the `skill` tool
// offers exactly the analysis's recommendations (every provisioned skill
// when it recommends none); a loaded body lands as the tool result and
// is observable as a tool_call; a name outside the scope is a typed tool
// error the run survives.
func TestADKSkillToolLoadsOnDemand(t *testing.T) {
	t.Parallel()
	snap := func() registry.Snapshot {
		return registry.Snapshot{Skills: map[string]registry.SkillSpec{
			"sk1": {Name: "sk1", Description: "Research with ddg", Body: "# SKILL1 BODY\nuse it", Path: "/skills/sk1"},
			"sk2": {Name: "sk2", Description: "Make decks", Body: "# SKILL2 BODY", Path: "/skills/sk2"},
		}}
	}
	gen := &stepGen{}
	var sk1Desc string
	gen.next = func(call int, msgs []*schema.Message) *schema.Message {
		switch call {
		case 0:
			o := model.GetCommonOptions(nil, gen.opts[0]...)
			for _, ti := range o.Tools {
				if ti.Name == skillToolName {
					sk1Desc = ti.Desc
				}
			}
			if sk1Desc == "" {
				t.Fatal("the skill tool must be advertised")
			}
			return toolCallMsg(skillToolName, `{"skill":"sk1"}`)
		case 1:
			if res := lastToolResult(msgs); res == nil || !strings.Contains(res.Content, "SKILL1 BODY") {
				t.Fatalf("the skill body must land as the tool result: %q", res.Content)
			}
			return toolCallMsg(skillToolName, `{"skill":"sk2"}`) // outside the scope
		case 2:
			if res := lastToolResult(msgs); res == nil || !strings.Contains(res.Content, "not offered") {
				t.Fatalf("an out-of-scope skill must be a typed tool error the model sees: %q", res.Content)
			}
			return &schema.Message{Content: "done"}
		}
		return &schema.Message{Content: "done"}
	}
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, snap)
	a.SetRunOptions(RunOptions{ProgressiveSkills: true})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-skill", Goal: "adk-skill goal", Model: "prov/m1", Skills: []string{"sk1"}}))
	if !strings.Contains(sk1Desc, "sk1") || !strings.Contains(sk1Desc, "Research with ddg") || strings.Contains(sk1Desc, "sk2") || strings.Contains(sk1Desc, "Make decks") {
		t.Fatalf("scoped to the recommendation: %q", sk1Desc)
	}
	var skillCalls int
	for _, evt := range events {
		if evt.Kind == domain.EventToolCall && evt.ToolCall != nil && evt.ToolCall.Tool == skillToolName {
			skillCalls++
		}
	}
	if skillCalls != 2 {
		t.Fatalf("both skill loads are observable tool_calls: %d", skillCalls)
	}

	// No recommendation → every provisioned skill is offered (the fallback).
	gen2 := &stepGen{}
	var foundAll bool
	gen2.next = func(call int, msgs []*schema.Message) *schema.Message {
		if call == 0 {
			o := model.GetCommonOptions(nil, gen2.opts[0]...)
			for _, ti := range o.Tools {
				if ti.Name == skillToolName {
					foundAll = true
					if !strings.Contains(ti.Desc, "sk1") || !strings.Contains(ti.Desc, "sk2") {
						t.Fatalf("no recommendation offers every skill: %q", ti.Desc)
					}
				}
			}
		}
		return &schema.Message{Content: "done"}
	}
	a2 := newBudgetAdapter(t, func(string) chatGenerator { return gen2 }, snap)
	a2.SetRunOptions(RunOptions{ProgressiveSkills: true})
	drainEvents(t, mustRun(t, a2, domain.GoalRequest{SessionID: "adk-skill-all", Goal: "adk-skill-all goal", Model: "prov/m1", Skills: []string{}}))
	if !foundAll {
		t.Fatal("an analysis with no recommendation must still offer the skill tool (every provisioned skill)")
	}
}

// No analysis (nil Skills) → no skill tool at all, like the bodies path
// had no skills section; `tools: []` → no engine extras either (the
// slice-0 no-tools invariant holds for karta_skill and karta_read_file).
func TestADKNoSkillToolWithoutAnalysisOrWithNoTools(t *testing.T) {
	t.Parallel()
	snap := func() registry.Snapshot {
		return registry.Snapshot{
			Skills: map[string]registry.SkillSpec{"sk1": {Name: "sk1", Description: "d", Body: "b", Path: "/p"}},
			Agents: map[string]registry.AgentSpec{"bare": {Name: "bare", SystemPrompt: "B.", Description: "d", Tools: []string{}}},
		}
	}
	advertised := func(req domain.GoalRequest) []string {
		gen := &stepGen{}
		gen.next = func(int, []*schema.Message) *schema.Message { return &schema.Message{Content: "done"} }
		a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, snap)
		a.SetRunOptions(RunOptions{ProgressiveSkills: true, ToolResultCap: 4096})
		drainEvents(t, mustRun(t, a, req))
		var names []string
		for _, ti := range model.GetCommonOptions(nil, gen.opts[0]...).Tools {
			names = append(names, ti.Name)
		}
		return names
	}
	if names := advertised(domain.GoalRequest{SessionID: "adk-nil", Goal: "adk-nil goal", Model: "prov/m1"}); strings.Contains(strings.Join(names, ","), skillToolName) {
		t.Fatalf("no analysis → no skill tool: %v", names)
	}
	names := advertised(domain.GoalRequest{SessionID: "adk-bare", Goal: "adk-bare goal", Agent: "bare", Model: "prov/m1", Skills: []string{"sk1"}})
	if strings.Contains(strings.Join(names, ","), skillToolName) || strings.Contains(strings.Join(names, ","), readFileToolName) {
		t.Fatalf("tools: [] → no engine extras: %v", names)
	}
}

// fakeSummarizer is the kernel policy's model seam, scripted.
type fakeSummarizer struct {
	calls int
	got   string
	ref   compaction.ModelRef
}

func (f *fakeSummarizer) Summarize(_ context.Context, transcript string, ref compaction.ModelRef) (string, error) {
	f.calls++
	f.got, f.ref = transcript, ref
	return "SUMMARY TEXT", nil
}

// TestADKInRunCompactionEmitsCompaction: the thin bridge asks the kernel
// policy (internal/compaction behind domain.Compactor) before each model
// call; once over budget the oldest messages become one summary bound to
// the run's model, the recent tail (tool calls and results) stays
// verbatim, and the pass is a durable compaction_triggered event.
func TestADKInRunCompactionEmitsCompaction(t *testing.T) {
	t.Parallel()
	big := namedTool{name: "big", out: strings.Repeat("word ", 300)} // ~300 tokens a round
	sum := &fakeSummarizer{}
	policy := &compaction.Service{Summarizer: sum, KeepRecent: 2}
	gen := &stepGen{}
	gen.next = func(call int, msgs []*schema.Message) *schema.Message {
		if call < 3 { // three tool rounds: ~950 tokens of context by call 3
			return toolCallMsg("big", `{"round":`+string(rune('0'+call))+`}`)
		}
		return &schema.Message{Content: "done"}
	}
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, nil)
	a.toolProv = &liveProvider{ts: []tools.Tool{big}}
	a.SetRunOptions(RunOptions{SummarizeAt: 500})
	a.SetCompactorFactory(func(p, m string) domain.Compactor { return policy.For(compaction.ModelRef{Provider: p, Model: m}) })
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-sum", Goal: "adk-sum goal", Model: "prov/m1"}))
	var compactionEvt *domain.TimelineEvent
	for i := range events {
		if events[i].Kind == domain.EventCompactionTriggered {
			compactionEvt = &events[i]
		}
	}
	if compactionEvt == nil || compactionEvt.Compaction == nil {
		t.Fatalf("the in-run pass must be a compaction_triggered event: %+v", events)
	}
	st := compactionEvt.Compaction
	if st.Strategy != "summarize" || st.TokensBefore <= st.TokensAfter || st.MessagesBefore < st.MessagesAfter || st.TokensAfter == 0 {
		t.Fatalf("stats must report both sides: %+v", st)
	}
	durable, _ := a.st.Events("adk-sum", "", 50)
	var onRecord bool
	for _, e := range durable {
		if e.Kind == string(domain.EventCompactionTriggered) {
			onRecord = true
		}
	}
	if !onRecord {
		t.Fatal("the compaction must be durable")
	}
	// A context that keeps growing past the budget is compacted again at
	// the next call — the summaries always come from the run's own model.
	if sum.calls < 1 || sum.ref.Model != "m1" {
		t.Fatalf("summaries from the run's own model: calls %d ref %+v", sum.calls, sum.ref)
	}
	if !strings.Contains(sum.got, "tool") {
		t.Fatalf("the transcript names the tool round: %q", sum.got)
	}
	last := gen.calls[len(gen.calls)-1]
	// [instruction][summary][the verbatim tail: the LAST intent + its result].
	if len(last) != 4 || last[0].Role != schema.System || !strings.Contains(last[1].Content, compaction.SummaryHeader+"SUMMARY TEXT") || last[2].Role != schema.Assistant || len(last[2].ToolCalls) != 1 || !strings.Contains(last[2].ToolCalls[0].Function.Arguments, "round") || last[3].Role != schema.Tool || last[3].ToolName != "big" {
		t.Fatalf("the next call must see the instruction, the summary, then the structured tail intact: %+v", last)
	}
	if len(gen.calls) != 4 {
		t.Fatalf("three rounds then the answer: %d calls", len(gen.calls))
	}
}

// A blocked PreCompact hook (through the policy) skips the pass and says
// so on the stream — never a run failure.
func TestADKInRunCompactionBlockedByHook(t *testing.T) {
	t.Parallel()
	big := namedTool{name: "big", out: strings.Repeat("word ", 300)}
	policy := &compaction.Service{Summarizer: &fakeSummarizer{}, KeepRecent: 2, Hooks: blockingHooks{}}
	gen := &stepGen{}
	gen.next = func(call int, msgs []*schema.Message) *schema.Message {
		if call < 3 {
			return toolCallMsg("big", `{"round":`+string(rune('0'+call))+`}`)
		}
		return &schema.Message{Content: "done"}
	}
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, nil)
	a.toolProv = &liveProvider{ts: []tools.Tool{big}}
	a.SetRunOptions(RunOptions{SummarizeAt: 500})
	a.SetCompactorFactory(func(p, m string) domain.Compactor { return policy.For(compaction.ModelRef{Provider: p, Model: m}) })
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-blocked", Goal: "adk-blocked goal", Model: "prov/m1"}))
	var blocked, compacted bool
	for _, evt := range events {
		if strings.Contains(evt.Text, "blocked by a PreCompact hook") {
			blocked = true
		}
		if evt.Kind == domain.EventCompactionTriggered {
			compacted = true
		}
	}
	if !blocked || compacted || !strings.Contains(events[len(events)-1].Text, "run completed") {
		t.Fatalf("a block skips the pass and the run completes: %+v", events)
	}
}

type blockingHooks struct{}

func (blockingHooks) Run(context.Context, domain.HookEvent) (domain.HookDecision, error) {
	return domain.HookDecision{Action: domain.HookBlock}, nil
}

// TestADKReasoningEffortRidesTheCalls: an agent's reasoning_effort rides
// every model call as an option; an agent without one sends none.
func TestADKReasoningEffortRidesTheCalls(t *testing.T) {
	t.Parallel()
	snap := func() registry.Snapshot {
		return registry.Snapshot{Agents: map[string]registry.AgentSpec{
			"builder": {Name: "builder", SystemPrompt: "Build.", Description: "d", ReasoningEffort: registry.EffortLow},
			"writer":  {Name: "writer", SystemPrompt: "Write.", Description: "d"},
		}}
	}
	gens := map[string]*stepGen{}
	var mu sync.Mutex
	newModel := func(modelID string) chatGenerator {
		gen := &stepGen{}
		gen.next = func(call int, _ []*schema.Message) *schema.Message {
			if call == 0 {
				return toolCallMsg("clock.now", `{}`)
			}
			return &schema.Message{Content: "done"}
		}
		mu.Lock()
		gens[modelID] = gen
		mu.Unlock()
		return gen
	}
	a := newBudgetAdapter(t, newModel, snap)
	a.effortOpt = func(effort string) model.Option { return model.WithModel("effort:" + effort) }
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-effort", Goal: "adk-effort goal", Agent: "builder", Model: "prov/m-builder"}))
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-effort-plain", Goal: "adk-effort-plain goal", Agent: "writer", Model: "prov/m-plain"}))
	efforts := func(id string) []string {
		g := gens[id]
		g.mu.Lock()
		defer g.mu.Unlock()
		var out []string
		for _, opts := range g.opts {
			out = append(out, modelName(opts))
		}
		return out
	}
	if e := efforts("m-builder"); len(e) != 2 || e[0] != "effort:low" || e[1] != "effort:low" {
		t.Fatalf("every builder call carries its effort: %v", e)
	}
	if e := efforts("m-plain"); len(e) != 2 || e[0] != "" || e[1] != "" {
		t.Fatalf("an agent without an effort sends none: %v", e)
	}
}

// modelName reads the stand-in option's ModelName ("" when absent).
func modelName(opts []model.Option) string {
	if m := model.GetCommonOptions(nil, opts...).Model; m != nil {
		return *m
	}
	return ""
}

// R2-2 / G6: the offload root is created CANONICAL and held as an os.Root —
// escapes, a symlinked DIRECTORY inside the root, and traversal writes are
// all refused by the kernel-bounded root (no path-name races).
func TestADKOffloadRootIsCanonicalAndConfined(t *testing.T) {
	t.Parallel()
	o, err := newOffloadRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.close(); _ = os.RemoveAll(o.dir) })
	if real, err := filepath.EvalSymlinks(o.dir); err != nil || real != o.dir {
		t.Fatalf("newOffloadRoot must be canonical: %q vs %q (%v)", o.dir, real, err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(o.dir, "trunc")); err != nil { // a directory symlink INSIDE the root
		t.Fatal(err)
	}
	reader, writer := readFileTool{offload: o}, offloadBackend{offload: o}
	for _, p := range []string{filepath.Join(outside, "secret"), o.dir + "/../secret", "/etc/passwd", o.dir} {
		if res := reader.Exec(context.Background(), `{"path":`+quote(p)+`}`); res.Err == nil {
			t.Fatalf("%s must be refused: %+v", p, res)
		}
	}
	if res := reader.Exec(context.Background(), `{"path":`+quote(filepath.Join(o.dir, "trunc", "secret"))+`}`); res.Err == nil || strings.Contains(res.Output, "secret") {
		t.Fatalf("a symlinked directory out of the root must not be followed: %+v", res)
	}
	if err := writer.Write(context.Background(), &filesystem.WriteRequest{FilePath: o.dir + "/trunc/planted", Content: "x"}); err == nil {
		t.Fatal("a write through a symlinked directory out of the root must fail")
	}
	if err := writer.Write(context.Background(), &filesystem.WriteRequest{FilePath: o.dir + "/../escape", Content: "x"}); err == nil {
		t.Fatal("a traversal write must fail")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted")); !os.IsNotExist(err) {
		t.Fatal("nothing may be written outside the root")
	}
	// The legitimate path works.
	if err := writer.Write(context.Background(), &filesystem.WriteRequest{FilePath: filepath.Join(o.dir, "trunc2", "call-1"), Content: "payload"}); err != nil {
		t.Fatalf("a legitimate offload must succeed: %v", err)
	}
	if res := reader.Exec(context.Background(), `{"path":`+quote(filepath.Join(o.dir, "trunc2", "call-1"))+`}`); res.Err != nil || !strings.Contains(res.Output, "payload") {
		t.Fatalf("a legitimate read must succeed: %+v", res)
	}
}

// R2-3: a user tool carrying the reserved karta_ prefix is not offered,
// and the drop is reported once on the stream.
func TestADKReservedToolNamesAreNotOffered(t *testing.T) {
	t.Parallel()
	gen := &stepGen{}
	gen.next = func(int, []*schema.Message) *schema.Message { return &schema.Message{Content: "done"} }
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, nil)
	a.toolProv = &liveProvider{ts: []tools.Tool{namedTool{name: skillToolName, out: "impostor"}, namedTool{name: "fine", out: "ok"}}}
	a.SetRunOptions(RunOptions{ProgressiveSkills: true})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-reserved", Goal: "adk-reserved goal", Model: "prov/m1"}))
	var names []string
	for _, ti := range model.GetCommonOptions(nil, gen.opts[0]...).Tools {
		names = append(names, ti.Name)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, skillToolName) || !strings.Contains(joined, "fine") {
		t.Fatalf("the reserved NAME is dropped (no karta_skill from the user plane), others kept: %v", names)
	}
	var reported int
	for _, evt := range events {
		if strings.Contains(evt.Text, "reserved karta_ prefix") && strings.Contains(evt.Text, skillToolName) {
			reported++
		}
	}
	if reported != 1 {
		t.Fatalf("the drop is reported once: %d", reported)
	}
}

// R2-4: byte ranges make progress on multibyte text and survive extreme
// offsets; the continuation offset always advances.
func TestClampRangeEdges(t *testing.T) {
	t.Parallel()
	raw := []byte("aé日b") // a(1) é(2) 日(3) b(1) = 7 bytes
	cases := []struct{ start, limit, wantStart, wantEnd int }{
		{0, 1, 0, 1},             // ASCII
		{1, 1, 1, 3},             // a limit shorter than é still yields the whole rune
		{3, 1, 3, 6},             // same for 日
		{2, 1, 1, 3},             // a start inside é moves back to its lead byte
		{1 << 30, 1 << 30, 7, 7}, // beyond the content: empty, no overflow
		{-5, 2, 0, 1},            // clamped to 0; a limit ending inside é yields "a" only
	}
	for _, c := range cases {
		start := c.start
		if start < 0 {
			start = 0
		}
		endWanted := len(raw)
		if c.limit < len(raw)-start {
			endWanted = start + c.limit
		}
		gotStart, gotEnd := clampRange(raw, c.start, endWanted)
		if gotStart != c.wantStart || gotEnd != c.wantEnd {
			t.Fatalf("clampRange(%d, limit %d) = [%d,%d), want [%d,%d)", c.start, c.limit, gotStart, gotEnd, c.wantStart, c.wantEnd)
		}
	}
}
