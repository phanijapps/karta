package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine/contracttest"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
)

// fakeProviderStore satisfies the adapter's ProviderStore seam with a
// canned provider — no live credential, no network.
type fakeProviderStore struct{}

func (fakeProviderStore) Lookup(name string) (providers.Provider, error) {
	return providers.Provider{Name: name, Type: "openai", BaseURL: "http://127.0.0.1:0"}, nil
}

func (fakeProviderStore) ResolvedLookup(_ context.Context, name string) (providers.Provider, string, error) {
	return providers.Provider{Name: name, Type: "openai", BaseURL: "http://127.0.0.1:0"}, "test-token", nil
}

func (fakeProviderStore) Secrets() providers.SecretStore { return fakeSecrets{} }

type fakeSecrets struct{}

func (fakeSecrets) Set(context.Context, string, string) error { return nil }
func (fakeSecrets) Get(context.Context, string) (string, error) {
	return "", errors.New("no secrets in tests")
}
func (fakeSecrets) Delete(context.Context, string) error { return nil }

// genMode is the fake model's fixed behavior for its whole lifetime.
type genMode int

const (
	genQuick genMode = iota
	genFail
	genBlocked
)

// fakeGenerator stands in for eino's chat model: zero network, behavior
// fixed at creation. One generator per model name (the adapter's model
// cache keys on provider/model/token), so each scenario uses its own
// model id — no cross-run state.
type fakeGenerator struct {
	mode    genMode
	gate    chan struct{} // blocked mode: released by send
	enter   chan struct{} // blocked mode: closed on first Generate call (test barrier)
	entered bool
	onMsgs  func([]*schema.Message) // optional prompt capture (tests)
}

func (g *fakeGenerator) Generate(ctx context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	switch g.mode {
	case genFail:
		return nil, fmt.Errorf("fake: model error")
	case genBlocked:
		// Signal entry once so tests can PROVE the run is live inside
		// the model before acting (deterministic overlap barriers).
		if g.enter != nil && !g.entered {
			g.entered = true
			close(g.enter)
		}
		select {
		case <-g.gate:
			return &schema.Message{Role: schema.Assistant, Content: "released"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	default:
		if g.onMsgs != nil {
			g.onMsgs(msgs)
		}
		// Tiny sleep widens the race window for nextID under -race.
		time.Sleep(2 * time.Millisecond)
		return &schema.Message{Role: schema.Assistant, Content: "final answer"}, nil
	}
}

func (g *fakeGenerator) release() {
	select {
	case g.gate <- struct{}{}:
	default:
	}
}

// modelFactory builds per-model-id generators and records blocked ones so
// tests can release them deterministically.
type modelFactory struct {
	mu        sync.Mutex
	blocks    []*fakeGenerator
	lastEnter chan struct{} // entry signal of the most recent blocked generator
	// promptMu/prompts capture every quick generator's message list
	// (system-prompt composition tests).
	promptMu sync.Mutex
	prompts  [][]*schema.Message
}

// recordPrompt captures one Generate call's messages (thread-safe).
func (f *modelFactory) recordPrompt(msgs []*schema.Message) {
	cp := make([]*schema.Message, len(msgs))
	copy(cp, msgs)
	f.promptMu.Lock()
	defer f.promptMu.Unlock()
	f.prompts = append(f.prompts, cp)
}

// systemPrompts returns the captured calls' leading system messages.
func (f *modelFactory) systemPrompts() []string {
	f.promptMu.Lock()
	defer f.promptMu.Unlock()
	var out []string
	for _, msgs := range f.prompts {
		if len(msgs) > 0 && msgs[0].Role == schema.System {
			out = append(out, msgs[0].Content)
		}
	}
	return out
}

func (f *modelFactory) new(_ context.Context, _ providers.Provider, _, modelID string) (chatGenerator, error) {
	g := &fakeGenerator{onMsgs: f.recordPrompt}
	switch {
	case strings.Contains(modelID, "fail"):
		g.mode = genFail
	case strings.Contains(modelID, "block"):
		g.mode = genBlocked
		g.gate = make(chan struct{}, 1)
		g.enter = make(chan struct{})
		f.mu.Lock()
		f.blocks = append(f.blocks, g)
		f.lastEnter = g.enter
		f.mu.Unlock()
	default:
		g.mode = genQuick
	}
	return g, nil
}

// waitBlockedEntered blocks until the most recent blocked generator
// exists AND is provably inside its Generate call (or the timeout hits).
// The generator is created lazily on the adapter's model-cache miss,
// so existence itself can lag RunGoal's return.
func (f *modelFactory) waitBlockedEntered(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		f.mu.Lock()
		enter := f.lastEnter
		f.mu.Unlock()
		if enter != nil {
			select {
			case <-enter:
				return
			case <-deadline:
				t.Fatal("blocked run never entered its model call")
			}
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("blocked generator was never created — model factory not called")
		}
	}
}

// releaseBlocked unblocks every blocked generator created so far.
func (f *modelFactory) releaseBlocked() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.blocks {
		g.release()
	}
}

// newTestAdapter builds a real adapter over fakes: real store (temp
// SQLite), real bus, fake provider store, fake model factory. The DEFAULT
// id generator is kept — the F-22 race test must exercise the real id path.
func newTestAdapter(t *testing.T, mf *modelFactory) *Adapter {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "engine-test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := New(fakeProviderStore{}, db, eventbus.New(), tools.BuiltinProvider{}, nil)
	a.newModel = mf.new
	return a
}

// Scenario model ids: distinct cache keys give each scenario its own
// deterministic fake model.
const (
	modelQuick   = "prov/m-quick"
	modelFail    = "prov/m-fail"
	modelBlocked = "prov/m-block"
)

func goal(model, sessionID string) domain.GoalRequest {
	return domain.GoalRequest{SessionID: sessionID, Goal: "goal for " + sessionID, Model: model}
}

// drainUntilClosed reads until the channel closes or the timeout hits.
func drainUntilClosed(ch <-chan domain.TimelineEvent, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// TestContractSuite runs the shared seam conformance suite against the
// REAL adapter (fake deps, default id generator).
func TestContractSuite(t *testing.T) {
	contracttest.Suite(t, contracttest.Fixture{
		New: func(t *testing.T) contracttest.Instance {
			mf := &modelFactory{}
			a := newTestAdapter(t, mf)
			return contracttest.Instance{
				Engine: a,
				StartQuick: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
					return a.RunGoal(ctx, goal(modelQuick, sessionID))
				},
				StartBlocked: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, func(), error) {
					ch, err := a.RunGoal(ctx, goal(modelBlocked, sessionID))
					return ch, mf.releaseBlocked, err
				},
				StartFailing: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
					return a.RunGoal(ctx, goal(modelFail, sessionID))
				},
			}
		},
		ResumeImplemented: false, // pinned: adapter returns "not yet implemented"
		Delegation: &contracttest.DelegationFixture{
			DelegateAgent: "researcher",
			Engine:        suiteDelegEngine,
			StartUnknownTarget: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
				gen := &routingGenerator{scripts: map[string][]fakeGenStep{
					"delegate": {
						{toolCall: "delegate_agent", args: `{"agent":"ghost","task":"x"}`},
						{content: "recovered"},
					},
				}}
				a := newDelegatingAdapter(t, gen, agentSnap)
				return a.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "delegate " + sessionID, Model: "prov/m1"})
			},
			StartDepthRefused: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
				// A named agent with max_depth 0 CANNOT delegate even at
				// depth 0 (0 < 0 false): run under such an agent with a
				// script that tries — refusal must leave no delegation
				// events and the run still completes.
				gen := &routingGenerator{scripts: map[string][]fakeGenStep{
					"delegate": {
						{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"x"}`},
						{content: "recovered without delegating"},
					},
				}}
				snap := agentSnap() // researcher.max_depth = 0
				a := newDelegatingAdapter(t, gen, func() registry.Snapshot { return snap })
				return a.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "delegate " + sessionID, Agent: "researcher", Model: "prov/m1"})
			},
			ChildAnswer: "research findings: X is Y",
			StartDelegating: func(ctx context.Context, sessionID string) (<-chan domain.TimelineEvent, error) {
				gen := &routingGenerator{scripts: map[string][]fakeGenStep{
					"delegate": {
						{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task research X"}`},
						{content: "root synthesized"},
					},
					"child-task": {{content: "research findings: X is Y"}},
				}}
				a := newDelegatingAdapter(t, gen, agentSnap)
				suiteDelegEngine.eng = a
				return a.RunGoal(ctx, domain.GoalRequest{SessionID: sessionID, Goal: "delegate " + sessionID, Model: "prov/m1"})
			},
		},
		FailureEventPredicate: func(evt domain.TimelineEvent) bool {
			return evt.Kind == domain.EventAgentStep && strings.Contains(evt.Text, "model error")
		},
		CompletionEventPredicate: func(evt domain.TimelineEvent) bool {
			return evt.Kind == domain.EventSession && strings.Contains(evt.Text, "run completed")
		},
	})
}

// TestCallerContextDetached pins F-23's CURRENT (buggy) semantics: the
// run context derives from context.Background(), so cancelling the ctx
// passed to RunGoal does NOT stop the run — only Cancel does. Flip this
// test when the backend-engine-run-ownership follow-up fixes detachment.
func TestCallerContextDetached(t *testing.T) {
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)
	ctx, cancelCaller := context.WithCancel(context.Background())
	ch, err := a.RunGoal(ctx, goal(modelBlocked, "detach-1"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Barrier: prove run 1 is live inside its model call BEFORE cancelling.
	mf.waitBlockedEntered(t)
	cancelCaller()
	// Characterization: the run stays alive despite caller cancellation.
	if drainUntilClosed(ch, 250*time.Millisecond) {
		t.Fatal("run ended on caller cancellation — detachment semantics changed?")
	}
	if err := a.Cancel(context.Background(), "detach-1"); err != nil {
		t.Fatalf("engine cancel: %v", err)
	}
	if !drainUntilClosed(ch, 5*time.Second) {
		t.Fatal("engine Cancel did not end the detached run")
	}
}

// TestSameSessionOverlapOrphansFirstRun pins F-24's CURRENT (buggy)
// semantics: a second RunGoal on the same session overwrites the first
// run's cancel registration, and the second run's exit deletes it — so
// the first run becomes uncancellable via Cancel. Flip when the follow-up
// lands a run-ownership policy.
func TestSameSessionOverlapOrphansFirstRun(t *testing.T) {
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)

	// Run 1: blocks inside the model.
	ch1, err := a.RunGoal(context.Background(), goal(modelBlocked, "overlap"))
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	// Barrier: run 1 is provably inside Generate before run 2 starts —
	// the overlap is deterministic, not a scheduling race.
	mf.waitBlockedEntered(t)
	// Run 2: same session, different model (immediate final answer) —
	// its RunGoal overwrites run 1's cancel registration.
	ch2, err := a.RunGoal(context.Background(), goal(modelQuick, "overlap"))
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	if !drainUntilClosed(ch2, 5*time.Second) {
		t.Fatal("run2 never completed")
	}

	// Cancel targets the session — but run 2's exit already deleted the
	// (overwritten) registration. Run 1 must still be alive.
	if err := a.Cancel(context.Background(), "overlap"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if drainUntilClosed(ch1, 250*time.Millisecond) {
		t.Fatal("run1 ended via Cancel — ownership semantics changed?")
	}

	// Manual release proves run 1 was merely orphaned, not dead.
	mf.releaseBlocked()
	if !drainUntilClosed(ch1, 5*time.Second) {
		t.Fatal("run1 never completed after manual release")
	}
}

// TestConcurrentRunsDefaultIDGenUnique exercises F-22: concurrent runs
// using the DEFAULT id generator under -race. Before the atomic fix this
// test trips the race detector and can mint duplicate event ids.
func TestConcurrentRunsDefaultIDGenUnique(t *testing.T) {
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)

	const runs = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, err := a.RunGoal(context.Background(), goal(modelQuick, fmt.Sprintf("race-%d", i)))
			if err != nil {
				t.Errorf("run %d: %v", i, err)
				return
			}
			// Bounded per-run drain: a wedged stream is a bounded failure,
			// never a hang (mirrors the shared suite's discipline).
			deadline := time.After(5 * time.Second)
			for done := false; !done; {
				select {
				case evt, ok := <-ch:
					if !ok {
						done = true
						break
					}
					mu.Lock()
					if evt.ID != "" && ids[evt.ID] {
						mu.Unlock()
						t.Errorf("duplicate event id from concurrent runs: %q (F-22 regression)", evt.ID)
						return
					}
					ids[evt.ID] = true
					mu.Unlock()
				case <-deadline:
					t.Errorf("run %d: stream did not close in time", i)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if len(ids) < runs {
		t.Fatalf("expected >=%d distinct events, got %d", runs, len(ids))
	}
}

// assertRunCompleted proves the run reached its FINAL answer (not an
// early death that merely closed the channel): the persisted history
// must contain the assistant's final message.
func assertRunCompleted(t *testing.T, a *Adapter, sessionID string) {
	t.Helper()
	msgs, err := a.st.Replay(sessionID, 0, 64)
	if err != nil {
		t.Fatalf("replay %s: %v", sessionID, err)
	}
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "final answer") {
			return
		}
	}
	t.Fatalf("run %s never produced its final assistant message (early closure, not completion): %d msgs", sessionID, len(msgs))
}

// TestAdapterPublishesDespiteAppendFailure pins the ADAPTER half of
// F-15's OPPOSITE-site semantics: when AppendEvent fails, the adapter
// still publishes to the bus (the chat site skips — see chat/emit_test.go).
// Deterministic poison: the idGen returns one constant ID whose event is
// pre-inserted, so every AppendEvent hits the primary-key constraint
// while the store (sessions, messages) and bus stay fully live.
func TestAdapterPublishesDespiteAppendFailure(t *testing.T) {
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)
	const dupID = "evt-dup-pin"
	a.idGen = func() string { return dupID } // every event append collides

	// Pre-insert the colliding event so AppendEvent fails on PK.
	if err := a.st.AppendEvent(store.Event{
		ID: dupID, SessionID: "append-fail", ExecutionID: "x", Kind: "session", Payload: "{}",
	}); err != nil {
		t.Fatalf("pre-insert: %v", err)
	}
	sub := a.bus.Subscribe(context.Background(), "append-fail")

	ch, err := a.RunGoal(context.Background(), goal(modelQuick, "append-fail"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !drainUntilClosed(ch, 5*time.Second) {
		t.Fatal("run did not complete despite append failures")
	}
	assertRunCompleted(t, a, "append-fail")
	// Publish-anyway: the subscriber received the unpersisted event.
	received := false
	deadline := time.After(time.Second)
	for !received {
		select {
		case evt, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed before any event arrived — publish-anyway semantics changed")
			}
			if evt.ID == dupID && evt.SessionID == "append-fail" {
				received = true
			}
		case <-deadline:
			t.Fatal("no event published despite AppendEvent failure — adapter-side F-15 semantics changed")
		}
	}
}

// TestAdapterEmitSurvivesClosedBus also asserts the durable half: with
// the bus closed, the run still completes AND its events persist to the
// Timeline Tree (the swallowed Publish must not lose durability).
func TestAdapterEmitSurvivesClosedBus(t *testing.T) {
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)
	a.bus.Close() // Publish hereafter returns ErrClosed.
	ch, err := a.RunGoal(context.Background(), goal(modelQuick, "closed-bus"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !drainUntilClosed(ch, 5*time.Second) {
		t.Fatal("run died on a closed bus — publish swallow semantics changed?")
	}
	assertRunCompleted(t, a, "closed-bus")
	events, err := a.st.Events("closed-bus", "", 16)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no durable events persisted on the closed-bus path")
	}
}

// --- Delegation (add-engine-delegation) --------------------------------------
//
// The fake generator routes by goal marker: "delegate" rounds call the
// delegate_agent tool; child goals ("child-task") answer directly.

func agentSnap() registry.Snapshot {
	snap := registry.Snapshot{Agents: map[string]registry.AgentSpec{}}
	snap.Agents["researcher"] = registry.AgentSpec{
		Name: "researcher", SystemPrompt: "You research.", Description: "Gathers evidence.",
		Model: "prov/m-research", Tools: []string{"clock_now"}, MaxDepth: 0,
	}
	snap.Agents["planner"] = registry.AgentSpec{
		Name: "planner", SystemPrompt: "You plan.", Description: "Plans work.", MaxDepth: 1,
	}
	return snap
}

func newDelegatingAdapter(t *testing.T, gen *routingGenerator, snapFn func() registry.Snapshot) *Adapter {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "deleg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := New(fakeProviderStore{}, db, eventbus.New(), tools.BuiltinProvider{}, snapFn)
	a.newModel = func(context.Context, providers.Provider, string, string) (chatGenerator, error) {
		return gen, nil
	}
	return a
}

func TestAgentNamedRunUsesAgentsWiring(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{}}
	a := newDelegatingAdapter(t, gen, agentSnap)
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{
		SessionID: "agent-run", Goal: "quick agent-run", Agent: "researcher",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	// The child's model was the agent's override and the run completed.
	msgs, _ := a.st.Replay("agent-run", 0, 8)
	if len(msgs) == 0 {
		t.Fatal("no rows")
	}
	// System prompt used: capture via the fake is not wired; the store's
	// provenance proves the model override instead.
	last := msgs[len(msgs)-1]
	if last.Model != "m-research" || last.Provider != "prov" {
		t.Fatalf("agent model override not used: %+v", last)
	}
}

func TestAgentNamedRunUnknownFailsTyped(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{}}
	a := newDelegatingAdapter(t, gen, agentSnap)
	_, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "x", Goal: "g", Agent: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "researcher") {
		t.Fatalf("unknown agent must fail typed listing configured agents: %v", err)
	}
}

func TestDelegateAndFoldBack(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{}}
	a := newDelegatingAdapter(t, gen, agentSnap)

	// Root ("delegate ...") delegates to researcher; the child goal
	// ("child-task ...") answers directly.
	childAnswer := "research findings: X is Y"
	gen.scripts = map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task research X"}`},
			{content: "root synthesized: " + childAnswer},
		},
		"child-task": {{content: childAnswer}},
	}

	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{
		SessionID: "deleg-1", Goal: "delegate root goal", Model: "prov/m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawDelegation, sawAnswer bool
	for evt := range ch {
		if evt.Kind == domain.EventDelegation {
			sawDelegation = true
		}
		if evt.Kind == domain.EventAgentStep && strings.Contains(evt.Text, childAnswer) {
			sawAnswer = true
		}
	}
	if !sawDelegation {
		t.Fatal("no delegation events in the parent stream")
	}
	if !sawAnswer {
		t.Fatal("child answer did not fold back into the parent stream")
	}
	// The child execution exists with parent linkage.
	var childExecs int
	rows, _ := store.QueryChildExecutions(a.st, "deleg-1")
	childExecs = len(rows)
	if childExecs != 1 {
		t.Fatalf("want 1 child execution, got %d", childExecs)
	}
}

func TestDelegateUnknownAgentIsTypedToolError(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{}}
	a := newDelegatingAdapter(t, gen, agentSnap)
	gen.scripts = map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"ghost","task":"anything"}`},
			{content: "recovered"},
		},
	}
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "deleg-2", Goal: "delegate unknown", Model: "prov/m1"})
	if err != nil {
		t.Fatal(err)
	}
	drainUntilClosed(ch, 5*time.Second)
	// The run recovered (final answer recorded despite the typed error).
	msgs, _ := a.st.Replay("deleg-2", 0, 16)
	found := false
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "recovered" {
			found = true
		}
	}
	if !found {
		t.Fatal("parent run did not continue past the unknown-agent tool error")
	}
}

func TestDelegationDepthGrantAndCap(t *testing.T) {
	t.Parallel()
	// Grant semantics: canDelegate = depth < spec.MaxDepth. Root (depth 0,
	// grant = cap 2) delegates to planner; planner (MaxDepth 2) at depth 1
	// MAY delegate once (1 < 2, and 1 < cap 2); the grandchild researcher
	// (MaxDepth 0) at depth 2 cannot (2 < 0 false, 2 < cap false) — and the
	// researcher spec in agentSnap is what enforces depth refusal.
	snap := agentSnap()
	snap.Agents["planner"] = registry.AgentSpec{
		Name: "planner", SystemPrompt: "You plan.", Description: "Plans work.", MaxDepth: 2,
	}
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{}}
	a := newDelegatingAdapter(t, gen, func() registry.Snapshot { return snap })
	gen.scripts = map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"planner","task":"plan it now"}`},
			{content: "root done"},
		},
		"plan it": {
			{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task deep"}`},
			{content: "planner done"},
		},
		"child-task": {{content: "deep answer"}},
	}
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "deleg-3", Goal: "delegate nested", Model: "prov/m1"})
	if err != nil {
		t.Fatal(err)
	}
	drainUntilClosed(ch, 10*time.Second)
	rows, _ := store.QueryChildExecutions(a.st, "deleg-3")
	if len(rows) != 2 {
		t.Fatalf("want 2 child executions (planner + researcher), got %d", len(rows))
	}
}

// fakeGenStep is one scripted generator turn: a final answer (content) or
// a tool call (toolCall+args).
type fakeGenStep struct {
	content  string
	toolCall string
	args     string
}

// routingGenerator scripts completions BY GOAL MARKER: the first user
// message's content picks the script; steps play in order, the last
// repeats. Parent/child flows route naturally ("delegate ...", "plan it",
// "child-task ...").
type routingGenerator struct {
	mu      sync.Mutex
	scripts map[string][]fakeGenStep
	calls   map[string]int // per-marker call index (advances the script)
}

func (g *routingGenerator) Generate(_ context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	marker := ""
	for _, m := range msgs {
		if m.Role == schema.User {
			marker = m.Content
			break
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	script := g.scripts["*"]
	for prefix, sc := range g.scripts {
		if prefix != "*" && strings.HasPrefix(marker, prefix) {
			script = sc
			break
		}
	}
	if len(script) == 0 {
		return &schema.Message{Role: schema.Assistant, Content: "done"}, nil
	}
	if g.calls == nil {
		g.calls = map[string]int{}
	}
	idx := g.calls[marker]
	g.calls[marker] = idx + 1
	if idx >= len(script) {
		idx = len(script) - 1
	}
	step := script[idx]
	if step.toolCall != "" {
		msg := &schema.Message{Role: schema.Assistant}
		msg.ToolCalls = []schema.ToolCall{{
			ID: fmt.Sprintf("call-%d", nextFakeCallID.Add(1)), Type: "function",
			Function: schema.FunctionCall{Name: step.toolCall, Arguments: step.args},
		}}
		return msg, nil
	}
	return &schema.Message{Role: schema.Assistant, Content: step.content}, nil
}

var nextFakeCallID atomic.Int64

// suiteDelegEngine is the delegation fixture's engine holder (set on
// first StartDelegating).
var suiteDelegEngine = &delegEngineHolder{}

// delegEngineHolder lets the delegation fixture's Cancel case reach the
// adapter built inside StartDelegating (set on first start).
type delegEngineHolder struct{ eng *Adapter }

func (h *delegEngineHolder) Name() string { return "eino-deleg" }
func (h *delegEngineHolder) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	return h.eng.RunGoal(ctx, req)
}
func (h *delegEngineHolder) Resume(ctx context.Context, sessionID string, input domain.UserInput) (<-chan domain.TimelineEvent, error) {
	return h.eng.Resume(ctx, sessionID, input)
}
func (h *delegEngineHolder) Cancel(ctx context.Context, sessionID string) error {
	return h.eng.Cancel(ctx, sessionID)
}

// --- Gate-fix tests (P1/P2 from the delegation review) -----------------------

// P1-1: offered tool defs carry REAL parameter schemas — a provider sees
// delegate_agent's required agent/task properties.
func TestDelegateToolParamsExposed(t *testing.T) {
	t.Parallel()
	ti := &capturedToolInfos{}
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task x"}`},
			{content: "done"},
		},
		"child-task": {{content: "child answer"}},
	}}
	a := newDelegatingAdapter(t, gen, agentSnap)
	a.newModel = func(context.Context, providers.Provider, string, string) (chatGenerator, error) {
		return ti, nil
	}
	ch, _ := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "params", Goal: "delegate params", Model: "prov/m1"})
	drainUntilClosed(ch, 5*time.Second)
	ti.mu.Lock()
	defer ti.mu.Unlock()
	var delegate *schema.ToolInfo
	for _, info := range ti.infos {
		if info.Name == "delegate_agent" {
			delegate = info
		}
	}
	if delegate == nil {
		t.Fatal("delegate_agent not offered")
	}
	if delegate.ParamsOneOf == nil {
		t.Fatal("no params on delegate_agent")
	}
	// The JSON-schema conversion: agent + task present.
	js, err := delegate.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(js.Properties)
	if !strings.Contains(string(raw), "agent") || !strings.Contains(string(raw), "task") {
		t.Fatalf("delegate params missing agent/task: %s", raw)
	}
}

// capturedToolInfos records offered tool infos (the fake generator that
// OBSERVES options — proving what a provider would receive).
type capturedToolInfos struct {
	mu      sync.Mutex
	infos   []*schema.ToolInfo
	sawCall bool
}

func (c *capturedToolInfos) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	c.mu.Lock()
	c.sawCall = true
	if o != nil {
		c.infos = o.Tools // nil (no WithTools) or the offered slice
	}
	c.mu.Unlock()
	// Delegate immediately (routing by first user message not available
	// here): answer directly; the offering capture is what this fake is for.
	return &schema.Message{Role: schema.Assistant, Content: "captured"}, nil
}

// P1-4: tools: [] is an explicit NO-TOOLS inventory.
func TestExplicitEmptyAllowlistGrantsNoTools(t *testing.T) {
	t.Parallel()
	gen := &capturedToolInfos{}
	snap := agentSnap()
	noTools := registry.AgentSpec{Name: "bare", SystemPrompt: "No tools.", Description: "Bare.", Tools: []string{}, MaxDepth: 0}
	snap.Agents["bare"] = noTools
	a := newDelegatingAdapter(t, &routingGenerator{scripts: map[string][]fakeGenStep{}}, func() registry.Snapshot { return snap })
	a.newModel = func(context.Context, providers.Provider, string, string) (chatGenerator, error) {
		return gen, nil
	}
	ch, _ := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "bare", Goal: "bare run", Agent: "bare", Model: "prov/m1"})
	drainUntilClosed(ch, 5*time.Second)
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if !gen.sawCall {
		t.Fatal("no generation observed — test broken")
	}
	if len(gen.infos) != 0 {
		t.Fatalf("tools: [] must offer NO inventory tools, got %d", len(gen.infos))
	}
	// INTENDED semantics: delegate is depth-granted MACHINERY, not
	// inventory — tools:[] + max_depth>0 is a pure delegating orchestrator
	// (the planner pattern). Pin it.
	orch := registry.AgentSpec{Name: "orchestrator", SystemPrompt: "Delegate everything.",
		Description: "Pure orchestrator.", Tools: []string{}, MaxDepth: 1}
	snapO := agentSnap()
	snapO.Agents["orchestrator"] = orch
	genO := &capturedToolInfos{}
	aO := newDelegatingAdapter(t, &routingGenerator{scripts: map[string][]fakeGenStep{}}, func() registry.Snapshot { return snapO })
	aO.newModel = func(context.Context, providers.Provider, string, string) (chatGenerator, error) {
		return genO, nil
	}
	chO, _ := aO.RunGoal(context.Background(), domain.GoalRequest{SessionID: "orch", Goal: "orchestrate", Agent: "orchestrator", Model: "prov/m1"})
	drainUntilClosed(chO, 5*time.Second)
	genO.mu.Lock()
	defer genO.mu.Unlock()
	if len(genO.infos) != 1 || genO.infos[0].Name != "delegate_agent" {
		var names []string
		for _, ti := range genO.infos {
			names = append(names, ti.Name)
		}
		t.Fatalf("orchestrator (tools:[], max_depth:1) must be offered ONLY delegate_agent, got %v", names)
	}
}

// P1-2: a child that must USE a tool result to answer — the child's
// conversation feeds results back across steps.
func TestChildUsesToolResult(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task use tools"}`},
			{content: "root done"},
		},
		"child-task": {
			{toolCall: "clock_now", args: "{}"},
			{content: "the tool said something"},
		},
	}}
	// The researcher's allowlist includes clock_now; the routing generator
	// answers with content referencing the round — proving the loop advanced
	// (the tool result was consumed, not repeated to the step bound).
	a := newDelegatingAdapter(t, gen, agentSnap)
	ch, _ := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "child-tool", Goal: "delegate now", Model: "prov/m1"})
	drainUntilClosed(ch, 5*time.Second)
	msgs, _ := a.st.Replay("child-tool", 0, 32)
	var childToolRows, childFinal int
	for _, m := range msgs {
		if strings.Contains(m.Content, "the tool said something") && m.Role == "assistant" {
			childFinal++
		}
		if m.ToolName == "clock_now" {
			childToolRows++
		}
	}
	if childToolRows == 0 || childFinal == 0 {
		t.Fatalf("child must execute the tool AND answer after: toolRows=%d finals=%d", childToolRows, childFinal)
	}
}

// P2-5: description-less agents refuse delegation at invocation.
func TestDescriptionlessAgentRefusesDelegation(t *testing.T) {
	t.Parallel()
	snap := agentSnap()
	snap.Agents["quiet"] = registry.AgentSpec{Name: "quiet", SystemPrompt: "Shh."} // no description
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"quiet","task":"x"}`},
			{content: "recovered"},
		},
	}}
	a := newDelegatingAdapter(t, gen, func() registry.Snapshot { return snap })
	ch, _ := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "quiet", Goal: "delegate quiet", Model: "prov/m1"})
	var refused bool
	for evt := range ch {
		if evt.ToolCall != nil && strings.Contains(evt.ToolCall.Output, "not delegatable") {
			refused = true
		}
	}
	if !refused {
		t.Fatal("description-less agent must refuse delegation at invocation")
	}
}

// P1-3: the PARENT's provider context excludes child transcripts.
func TestParentContextExcludesChildMessages(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{
		"delegate": {
			{toolCall: "delegate_agent", args: `{"agent":"researcher","task":"child-task secret work"}`},
			{content: "root done"},
		},
		"child-task": {{content: "CHILD-TRANSCRIPT-MARKER"}},
	}}
	a := newDelegatingAdapter(t, gen, agentSnap)
	ch, _ := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "ctx", Goal: "delegate ctx", Model: "prov/m1"})
	drainUntilClosed(ch, 5*time.Second)
	// The parent's ROOT window contains ONLY root-execution rows.
	rows, err := a.st.RootWindow("ctx", "exec-root-ctx", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rows {
		if m.ExecutionID != "exec-root-ctx" {
			t.Fatalf("root window leaked execution %s", m.ExecutionID)
		}
		if m.Role == "user" && strings.Contains(m.Content, "secret work") {
			t.Fatal("child task leaked into the parent's provider context")
		}
	}
}

// newWardVault bootstraps a temp vault with one enterable ward (mirrors
// the wardtools test fixture).
func newWardVault(t *testing.T) *wards.Vault {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wards")
	if err := wards.Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("vault bootstrap: %v", err)
	}
	ward := filepath.Join(root, "stock-analysis")
	if err := os.MkdirAll(ward, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ward-conf.yaml":    "archetype: research\nlayout:\n  kind: directory\n  children:\n    - { id: canonical, match: \"{ward}.md\", kind: file, format: markdown }\n",
		"stock-analysis.md": "# Stock Analysis\nthe canonical page\n",
		"AGENTS.md":         "# Working here\n\nsources/ is immutable intake.\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(ward, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vault, err := wards.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("vault open: %v", err)
	}
	return vault
}

// TestHostedRootRunComposesActiveWard (add-chat-modes): a root run in a
// session whose host carries a ward executes as the WARD's agent — the
// ## Active Ward section (constitution + layout packet) joins the system
// prompt. EnsureWard (restart recovery from the session row's binding)
// pre-enters and fires the binder.
func TestHostedRootRunComposesActiveWard(t *testing.T) {
	vault := newWardVault(t)
	mf := &modelFactory{}
	a := newTestAdapter(t, mf)
	a.SetWardVault(func() *wards.Vault { return vault })
	var mu sync.Mutex
	var binds []string
	a.SetWardBinder(func(sessionID, ward string) {
		mu.Lock()
		defer mu.Unlock()
		binds = append(binds, sessionID+"/"+ward)
	})

	// Restart recovery: the binding pre-enters the ward on a fresh host.
	if err := a.EnsureWard(context.Background(), "s-ward", "stock-analysis"); err != nil {
		t.Fatalf("EnsureWard: %v", err)
	}
	// Idempotent: a host that already carries the ward keeps it.
	if err := a.EnsureWard(context.Background(), "s-ward", "stock-analysis"); err != nil {
		t.Fatalf("EnsureWard (idempotent): %v", err)
	}

	events, err := a.RunGoal(context.Background(), goal(modelQuick, "s-ward"))
	if err != nil {
		t.Fatalf("RunGoal: %v", err)
	}
	if !drainUntilClosed(events, 5*time.Second) {
		t.Fatal("run did not finish")
	}

	var system string
	for _, s := range mf.systemPrompts() {
		system += s + "\n---\n"
	}
	for _, want := range []string{"## Active Ward", "stock-analysis", "sources/ is immutable intake."} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(binds) != 1 || binds[0] != "s-ward/stock-analysis" {
		t.Fatalf("binder fired: %v (EnsureWard idempotence must not double-fire)", binds)
	}
}
