// Package compaction is the in-run context compaction policy behind
// domain.Compactor (the backend constitution's Compaction Rule): when a
// context exceeds its budget, the OLDEST messages are summarized by a
// Summarizer and the recent tail is kept verbatim. Framework-free — the
// engine adapters bridge it (Eino: mw_compaction.go) and never hold the
// policy. The daemon wires the Summarizer (a model caller) and the
// optional cheap-model override (compaction.summarizer — decision D4:
// a cheap model by default, else the run's own).
package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"zbot/backend/internal/domain"
)

// DefaultKeepRecent is how many messages stay verbatim after the summary;
// the tail also stays under two fifths of the budget in tokens.
const (
	DefaultKeepRecent           = 12
	keepTailFractionNumerator   = 2
	keepTailFractionDenominator = 5
)

// ModelRef names the model a summary is asked from.
type ModelRef struct{ Provider, Model string }

// Summarizer compresses a rendered transcript. The ref is the run's own
// model unless the service carries a DefaultModel override.
type Summarizer interface {
	Summarize(ctx context.Context, transcript string, ref ModelRef) (summary string, err error)
}

// Service implements domain.Compactor. The zero value never compacts.
type Service struct {
	Summarizer Summarizer
	// KeepRecent messages stay verbatim after the summary (0 = the default).
	KeepRecent int
	// DefaultModel, when set, summarizes instead of the per-call run model
	// (the cheap summarizer, D4).
	DefaultModel *ModelRef
	ref          ModelRef
	// Hooks are NOT raised here: the engine's compaction bridge raises
	// PreCompact through the hook runner (one emitting seam — the decision
	// reaches the Timeline with the run's attribution; hook-runner R3).
}

// For binds the service to a run's model (the per-call ref the Summarizer
// falls back to). The copy is cheap; the daemon's one service serves all runs.
func (s *Service) For(ref ModelRef) *Service {
	c := *s
	c.ref = ref
	return &c
}

// ShouldCompact reports whether the context is over its budget.
func (s *Service) ShouldCompact(_ context.Context, state domain.ContextState) bool {
	if s == nil || s.Summarizer == nil || state.Budget.MaxTotalTokens <= 0 {
		return false
	}
	return state.TotalTokens > state.Budget.MaxTotalTokens-state.Budget.ReserveOutput
}

// ErrNothingToSummarize is returned when every message is already in the
// kept tail (the bridge treats it as "skip", never as a run failure).
var ErrNothingToSummarize = errors.New("compaction: nothing older than the kept tail")

// Compact summarizes the messages between the leading system messages and
// the kept tail. CONTRACT (bridges rely on it): the result is the input's
// leading system messages verbatim, then ONE system summary message, then
// a VERBATIM SUFFIX of the input — so a bridge can keep its structured
// originals (tool calls, tool results) for the tail and only substitute
// the summary.
func (s *Service) Compact(ctx context.Context, messages []domain.Message, budget domain.TokenBudget) ([]domain.Message, domain.CompactionStats, error) {
	before := Tokens(messages)
	// The verbatim prefix is the instruction(s): leading system messages
	// that are not a previous pass's summary — a summary is folded into
	// the next one (one summary in the context, however many passes).
	prefix := 0
	for prefix < len(messages) && IsInstruction(messages[prefix]) {
		prefix++
	}
	// The kept tail: at most KeepRecent messages AND (when the budget says)
	// at most keepTailFraction of it in tokens — a tail that alone fills
	// the budget would make every pass pointless. Halve until both hold.
	keep := s.keepRecent()
	if keep > len(messages) {
		keep = len(messages)
	}
	keepTokens := 0
	if budget.MaxTotalTokens > 0 {
		keepTokens = budget.MaxTotalTokens * keepTailFractionNumerator / keepTailFractionDenominator
	}
	// Halve the tail while it would leave nothing to summarize, or while it
	// alone exceeds the token target.
	split := len(messages) - keep
	for keep > 1 && (split <= prefix || (keepTokens > 0 && Tokens(messages[split:]) > keepTokens)) {
		keep /= 2
		split = len(messages) - keep
	}
	if split < prefix {
		split = prefix
	}
	// Never cut inside an exchange: the tail may not START with a tool
	// result (its intent would be summarized away and the provider would
	// reject the orphan) — move the cut back to the message before it.
	for split > prefix && messages[split].Role == domain.RoleTool {
		split--
	}
	if split <= prefix {
		return messages, domain.CompactionStats{Strategy: "none", TokensBefore: before, TokensAfter: before, MessagesBefore: len(messages), MessagesAfter: len(messages)}, ErrNothingToSummarize
	}
	old, tail := messages[prefix:split], messages[split:]
	ref := s.ref
	if s.DefaultModel != nil {
		ref = *s.DefaultModel
	}
	summary, err := s.Summarizer.Summarize(ctx, Render(old), ref)
	if err != nil {
		return messages, domain.CompactionStats{Strategy: "error", TokensBefore: before, MessagesBefore: len(messages)}, fmt.Errorf("compaction: summarize: %w", err)
	}
	out := make([]domain.Message, 0, len(messages)-len(old)+1)
	out = append(out, messages[:prefix]...)
	out = append(out, domain.Message{Role: domain.RoleSystem, Content: SummaryHeader + strings.TrimSpace(summary)})
	out = append(out, tail...)
	return out, domain.CompactionStats{Strategy: "summarize", TokensBefore: before, TokensAfter: Tokens(out), MessagesBefore: len(messages), MessagesAfter: len(out)}, nil
}

func (s *Service) keepRecent() int {
	if s.KeepRecent > 0 {
		return s.KeepRecent
	}
	return DefaultKeepRecent
}

// SummaryHeader introduces the summary message the model sees.
const SummaryHeader = "## Summary of the earlier part of this run\n\n"

// IsInstruction reports whether a message belongs to the verbatim prefix:
// a system message that is not a previous summary.
func IsInstruction(m domain.Message) bool {
	return m.Role == domain.RoleSystem && !strings.HasPrefix(m.Content, SummaryHeader)
}

// Render turns messages into the transcript the summarizer reads: one
// "role: content" block per message, tool calls rendered inline.
func Render(msgs []domain.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n\n", m.Role, strings.TrimSpace(m.Content))
	}
	return b.String()
}

// Tokens is the policy's estimate: the message's own count when a bridge
// set one, else characters / 4.
func Tokens(msgs []domain.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Tokens > 0 {
			n += m.Tokens
		} else {
			n += len(m.Content) / 4
		}
	}
	return n
}

// Completer is the model seam the LLM summarizer talks to (the daemon's
// caller, the same one the intent analyst uses).
type Completer interface {
	Complete(ctx context.Context, provider, model, system, user string) (string, error)
}

// LLMSummarizer asks a chat model for the summary.
type LLMSummarizer struct{ Completer Completer }

const summaryInstruction = "You compress the EARLIER part of an agent run so the agent can continue with a short memory of it. " +
	"Keep every decision, fact (with its numbers), file path, command and its outcome, delegation and what it returned, and every open question. " +
	"Drop narration and reasoning text. Write at most 300 words, as plain notes. Output only the summary."

// Summarize implements Summarizer.
func (l LLMSummarizer) Summarize(ctx context.Context, transcript string, ref ModelRef) (string, error) {
	return l.Completer.Complete(ctx, ref.Provider, ref.Model, summaryInstruction, transcript)
}
