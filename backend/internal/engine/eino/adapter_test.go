package eino

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine/contracttest"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
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
}

func (g *fakeGenerator) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
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
}

func (f *modelFactory) new(_ context.Context, _ providers.Provider, _, modelID string) (chatGenerator, error) {
	g := &fakeGenerator{}
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
	a := New(fakeProviderStore{}, db, eventbus.New(), tools.BuiltinProvider{})
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
