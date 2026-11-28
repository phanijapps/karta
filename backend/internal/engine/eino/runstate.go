// Shared run budget + the ADK run state (extracted from adapter.go so
// the legacy loop and the ADK path bound a run identically).

package eino

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/progress"
)

// maxExtensions: productive agents can earn up to 3 windows.
const maxExtensions = 3

// budgetFor derives a run's step budgets from the request's bound: the
// soft budget (intent-derived, clamped to MaxSteps), the hard stop (2x
// soft) and the absolute ceiling (hard + the extension windows).
func budgetFor(maxSteps int) (soft, hard, steps int) {
	soft = maxSteps
	if soft <= 0 || soft > MaxSteps {
		soft = MaxSteps
	}
	hard = soft * 2
	return soft, hard, hard + maxExtensions*soft
}

// errStuck is the budget middleware's typed kill: the progress tracker
// says the agent is stuck (the driver names it on the stream).
type errStuck struct{ score, iterations int }

func (e errStuck) Error() string {
	return fmt.Sprintf("engine: stuck (score %d after %d iterations)", e.score, e.iterations)
}

// runState is what the ADK middlewares share for ONE run: the progress
// tracker, the budgets, the call/output counters and the usage totals.
type runState struct {
	soft, hard, steps int
	tracker           *progress.Tracker
	calls             int                 // model calls so far (BeforeModelRewriteState)
	outputs           int                 // assistant outputs seen (the nudge guard)
	suppress          bool                // tools suppressed on the current call (near the ceiling)
	nudgeAt           int                 // index of the transient progress nudge in state.Messages; -1 = none
	stripMsg          *schema.Message     // the continuation run's require_tool_use nudge (removed by identity after its first model call; root runs; nil = none)
	pendingIDs        map[string][]string // latest intent's tool name → queued call IDs (refusal rows pair with them)

	// reportedDrops remembers the reserved-name drops and wire-name
	// collisions already reported on the stream (once each per run), so a
	// live inventory change mid-run reports only what is new.
	reportedDrops map[string]bool

	// foreign names the tool infos middlewares add themselves (the skill
	// tool, injected at BeforeAgent): kept across calls by the budget's
	// refresh while the user's live inventory is re-derived. Captured once,
	// at the first call.
	foreign map[string]bool

	mu                  sync.Mutex
	tokensIn, tokensOut int64
}

func newRunState(maxSteps int) *runState {
	soft, hard, steps := budgetFor(maxSteps)
	return &runState{soft: soft, hard: hard, steps: steps, tracker: progress.New(), nudgeAt: -1}
}

// pendingID returns the next queued call ID for a tool name — the one a
// refusal of that name must pair with. Tool calls execute sequentially,
// so the order matches the intent's. Empty when none is queued.
func (s *runState) pendingID(name string) string {
	q := s.pendingIDs[name]
	if len(q) == 0 {
		return ""
	}
	s.pendingIDs[name] = q[1:]
	return q[0]
}

// addUsage accumulates one model call's usage onto the run totals.
func (s *runState) addUsage(rm *schema.ResponseMeta) {
	if rm == nil || rm.Usage == nil {
		return
	}
	s.mu.Lock()
	s.tokensIn += int64(rm.Usage.PromptTokens)
	s.tokensOut += int64(rm.Usage.CompletionTokens)
	s.mu.Unlock()
}

func (s *runState) usage() (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokensIn, s.tokensOut
}

// baseModelOf widens the cached chat generator to the model.BaseModel
// the ADK requires. Production models (the OpenAI-compatible client)
// already implement both calls; a Generate-only fake (tests) gets a
// Stream that materializes its single message — runs are non-streaming.
func baseModelOf(g chatGenerator) model.BaseModel[*schema.Message] {
	if bm, ok := g.(model.BaseModel[*schema.Message]); ok {
		return bm
	}
	return &generateOnly{g: g}
}

type generateOnly struct{ g chatGenerator }

func (o *generateOnly) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return o.g.Generate(ctx, msgs, opts...)
}

func (o *generateOnly) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := o.g.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}
