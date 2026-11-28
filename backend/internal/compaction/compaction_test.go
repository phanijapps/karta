package compaction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"zbot/backend/internal/domain"
)

type fakeSummarizer struct {
	got   string
	ref   ModelRef
	calls int
	err   error
}

func (f *fakeSummarizer) Summarize(_ context.Context, transcript string, ref ModelRef) (string, error) {
	f.calls++
	f.got, f.ref = transcript, ref
	if f.err != nil {
		return "", f.err
	}
	return "SUMMARY", nil
}

func msgs(n int) []domain.Message {
	out := make([]domain.Message, 0, n+2)
	out = append(out, domain.Message{Role: domain.RoleSystem, Content: "instruction"})
	for i := 0; i < n; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		out = append(out, domain.Message{Role: role, Content: strings.Repeat("x", 400)}) // 100 tokens each
	}
	return out
}

// ShouldCompact: over budget (less the reserve) and only with a summarizer.
func TestShouldCompact(t *testing.T) {
	s := &Service{Summarizer: &fakeSummarizer{}}
	over := domain.ContextState{TotalTokens: 1200, Budget: domain.TokenBudget{MaxTotalTokens: 1000, ReserveOutput: 100}}
	if !s.ShouldCompact(context.Background(), over) {
		t.Fatal("1200 tokens over a 900 budget must compact")
	}
	under := over
	under.TotalTokens = 900
	if s.ShouldCompact(context.Background(), under) {
		t.Fatal("at the budget there is nothing to do")
	}
	if (&Service{}).ShouldCompact(context.Background(), over) {
		t.Fatal("without a summarizer the service never compacts")
	}
}

// Compact honors the contract: leading system messages verbatim, ONE
// summary message, then a verbatim suffix that starts at a user message;
// the stats report both sides; the cheap DefaultModel wins over the run's.
func TestCompactContract(t *testing.T) {
	sum := &fakeSummarizer{}
	cheap := ModelRef{Provider: "ollama", Model: "glm-5.3-flash:cloud"}
	s := (&Service{Summarizer: sum, KeepRecent: 4, DefaultModel: &cheap}).For(ModelRef{Provider: "p", Model: "run-model"})
	in := msgs(12) // 1 system + 12 exchanges = 13 messages
	out, stats, err := s.Compact(context.Background(), in, domain.TokenBudget{})
	if err != nil {
		t.Fatal(err)
	}
	// 1202 = 2 (the instruction) + 12 × 100 tokens.
	if stats.Strategy != "summarize" || stats.TokensBefore != 1202 || stats.TokensAfter >= stats.TokensBefore || stats.MessagesBefore != 13 {
		t.Fatalf("stats: %+v", stats)
	}
	if len(out) != 1+1+4 {
		t.Fatalf("prefix + summary + a 4-message suffix: %d messages", len(out))
	}
	if out[0].Content != "instruction" || out[1].Role != domain.RoleSystem || !strings.HasPrefix(out[1].Content, SummaryHeader) || !strings.Contains(out[1].Content, "SUMMARY") {
		t.Fatalf("prefix and summary: %+v", out[:2])
	}
	// The suffix starts at a user message and is verbatim.
	if out[2].Role != domain.RoleUser {
		t.Fatalf("the tail starts an exchange (a user message): %+v", out[2])
	}
	tail := in[len(in)-(len(out)-2):]
	for i := 2; i < len(out); i++ {
		if out[i] != tail[i-2] {
			t.Fatalf("tail must be verbatim at %d", i)
		}
	}
	if sum.calls != 1 || sum.ref != cheap {
		t.Fatalf("one summary from the cheap model: calls %d ref %+v", sum.calls, sum.ref)
	}
	if !strings.Contains(sum.got, "user: ") || strings.Contains(sum.got, "instruction") {
		t.Fatalf("the transcript renders the old messages only (no system prefix): %q", sum.got)
	}
}

// The run's own model is used when no cheap override is configured (D4).
func TestCompactFallsBackToTheRunModel(t *testing.T) {
	sum := &fakeSummarizer{}
	s := (&Service{Summarizer: sum, KeepRecent: 2}).For(ModelRef{Provider: "p", Model: "run-model"})
	if _, _, err := s.Compact(context.Background(), msgs(6), domain.TokenBudget{}); err != nil {
		t.Fatal(err)
	}
	if sum.ref.Model != "run-model" {
		t.Fatalf("ref: %+v", sum.ref)
	}
}

// With nothing older than the system prefix and a one-message tail (the
// tail halves until something is left) → ErrNothingToSummarize, the input
// unchanged (a bridge skips, never fails the run).
func TestCompactNothingToSummarize(t *testing.T) {
	s := &Service{Summarizer: &fakeSummarizer{}, KeepRecent: 20}
	in := []domain.Message{{Role: domain.RoleSystem, Content: "i"}, {Role: domain.RoleUser, Content: "goal"}}
	out, stats, err := s.Compact(context.Background(), in, domain.TokenBudget{})
	if !errors.Is(err, ErrNothingToSummarize) || stats.Strategy != "none" || len(out) != len(in) {
		t.Fatalf("nothing to summarize: out %d stats %+v err %v", len(out), stats, err)
	}
}

type fakeHooks struct {
	got   domain.HookEvent
	block bool
	err   error
}

func (h *fakeHooks) Run(_ context.Context, e domain.HookEvent) (domain.HookDecision, error) {
	h.got = e
	if h.err != nil {
		return domain.HookDecision{}, h.err
	}
	if h.block {
		return domain.HookDecision{Action: domain.HookBlock, Reason: "no"}, nil
	}
	return domain.HookDecision{Action: domain.HookAllow}, nil
}

// PreCompact fires before the pass with the size of what is summarized;
// block skips it (strategy "blocked", input unchanged), an error surfaces.
func TestCompactHooks(t *testing.T) {
	hooks := &fakeHooks{block: true}
	sum := &fakeSummarizer{}
	in := msgs(8)
	s := &Service{Summarizer: sum, KeepRecent: 2, Hooks: hooks}
	out, stats, err := s.Compact(context.Background(), in, domain.TokenBudget{})
	if err != nil || stats.Strategy != "blocked" || len(out) != len(in) || sum.calls != 0 {
		t.Fatalf("a block skips the pass: stats %+v err %v calls %d", stats, err, sum.calls)
	}
	if hooks.got.Event != domain.HookPreCompact || hooks.got.Payload["messages"] != 6 { // 9 messages − the system − the 2-message tail (cut at the next user message)
		t.Fatalf("the hook sees the pass: %+v", hooks.got)
	}
	hooks.block, hooks.err = false, errors.New("hook boom")
	if _, _, err := s.Compact(context.Background(), in, domain.TokenBudget{}); err == nil {
		t.Fatal("a failing hook surfaces its error")
	}
}

// A failing summarizer returns the input unchanged with the error.
func TestCompactSummarizerError(t *testing.T) {
	s := &Service{Summarizer: &fakeSummarizer{err: errors.New("model down")}, KeepRecent: 2}
	in := msgs(6)
	out, stats, err := s.Compact(context.Background(), in, domain.TokenBudget{})
	if err == nil || stats.Strategy != "error" || len(out) != len(in) {
		t.Fatalf("summarizer failure: stats %+v err %v", stats, err)
	}
}

type fakeCompleter struct{ system, user string }

func (f *fakeCompleter) Complete(_ context.Context, _, _, system, user string) (string, error) {
	f.system, f.user = system, user
	return "ok", nil
}

func TestLLMSummarizer(t *testing.T) {
	c := &fakeCompleter{}
	if _, err := (LLMSummarizer{Completer: c}).Summarize(context.Background(), "T", ModelRef{Provider: "p", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if c.user != "T" || !strings.Contains(c.system, "at most 300 words") {
		t.Fatalf("the summarizer prompt: system %q user %q", c.system, c.user)
	}
}

// A tail that alone would fill the budget shrinks: with a 500-token budget
// the tail may hold at most 200 tokens (two fifths), so a 4-message tail
// of 100-token messages halves to 2.
func TestCompactTailIsTokenBounded(t *testing.T) {
	sum := &fakeSummarizer{}
	s := &Service{Summarizer: sum, KeepRecent: 4}
	in := msgs(10) // 1 system + 10 × 100 tokens
	out, stats, err := s.Compact(context.Background(), in, domain.TokenBudget{MaxTotalTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1+1+2 || stats.TokensAfter > 500 {
		t.Fatalf("the tail must shrink to the token bound: %d messages, stats %+v", len(out), stats)
	}
}

// The cut never lands on a tool result: with tool rounds in the kept tail,
// the tail grows BACK to the intent that owns its first result (the
// orphan a provider would reject).
func TestCompactNeverOrphansToolResults(t *testing.T) {
	sum := &fakeSummarizer{}
	in := []domain.Message{
		{Role: domain.RoleSystem, Content: "i"},
		{Role: domain.RoleUser, Content: "goal"},
		{Role: domain.RoleAssistant, Content: "", Tokens: 10}, // intent A
		{Role: domain.RoleTool, Content: strings.Repeat("r", 400), Tokens: 100},
		{Role: domain.RoleAssistant, Content: "", Tokens: 10}, // intent B
		{Role: domain.RoleTool, Content: strings.Repeat("r", 400), Tokens: 100},
	}
	out, stats, err := (&Service{Summarizer: sum, KeepRecent: 2}).Compact(context.Background(), in, domain.TokenBudget{MaxTotalTokens: 250}) // keep ≤ 100 tokens → the tail halves to 1 → moves back to the intent
	if err != nil {
		t.Fatal(err)
	}
	tail := out[len(out)-2:]
	if stats.Strategy != "summarize" || len(out) != 1+1+2 || tail[0].Role != domain.RoleAssistant || tail[1].Role != domain.RoleTool {
		t.Fatalf("the tail is [intent B, result B]: %+v", out)
	}
}

// A previous pass's summary is folded into the next one: two passes leave
// ONE summary message, not a stack of them.
func TestCompactFoldsPreviousSummary(t *testing.T) {
	sum := &fakeSummarizer{}
	s := &Service{Summarizer: sum, KeepRecent: 2}
	in := []domain.Message{{Role: domain.RoleSystem, Content: "instruction"}, {Role: domain.RoleSystem, Content: SummaryHeader + "old summary"}}
	in = append(in, msgs(4)[1:]...) // [instruction][old summary][u][a][u][a]
	out, _, err := s.Compact(context.Background(), in, domain.TokenBudget{})
	if err != nil {
		t.Fatal(err)
	}
	summaries := 0
	for _, m := range out {
		if strings.HasPrefix(m.Content, SummaryHeader) {
			summaries++
		}
	}
	if summaries != 1 || !strings.Contains(sum.got, "old summary") {
		t.Fatalf("one summary, folded from the previous: %d summaries, transcript %q", summaries, sum.got)
	}
}

// One oversized final exchange is kept whole even past the token target —
// a provider rejects an orphaned tool result, so the bound yields.
func TestCompactKeepsAnOversizedFinalExchange(t *testing.T) {
	sum := &fakeSummarizer{}
	in := []domain.Message{
		{Role: domain.RoleSystem, Content: "i"},
		{Role: domain.RoleUser, Content: "goal"},
		{Role: domain.RoleAssistant, Content: "", Tokens: 10},
		{Role: domain.RoleTool, Content: strings.Repeat("r", 4000), Tokens: 1000}, // one huge result
	}
	out, stats, err := (&Service{Summarizer: sum, KeepRecent: 4}).Compact(context.Background(), in, domain.TokenBudget{MaxTotalTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	tail := out[len(out)-2:]
	if tail[0].Role != domain.RoleAssistant || tail[1].Role != domain.RoleTool || stats.MessagesAfter != 4 {
		t.Fatalf("the final exchange stays whole: %+v", out)
	}
}
