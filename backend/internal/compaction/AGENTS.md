# compaction
The in-run context compaction policy behind `domain.Compactor` — the
backend constitution's Compaction Rule made real. Framework-free: it
operates on `domain.Message` and `domain.TokenBudget` only; engine
adapters bridge it (Eino: `internal/engine/eino/mw_compaction.go`) and
never hold the policy.
- `Service` implements `ShouldCompact` (over budget − reserve) and
  `Compact`: the leading instruction messages stay verbatim, a previous
  pass's summary is folded into the next one (`IsInstruction`), the
  messages between are summarized by a `Summarizer`, and the recent tail
  stays verbatim — TARGETS at most `KeepRecent` messages AND two fifths
  of the budget in tokens, both best-effort: the cut never starts on a
  tool result (an orphaned result is a provider error), so one oversized
  final exchange can exceed either bound.
- CONTRACT bridges rely on: `Compact` returns `[instruction prefix] +
  [ONE summary message] + [a verbatim suffix of the input]`, so a bridge
  keeps its structured originals (tool calls, tool results) for the tail.
- `Hooks` (a `domain.HookRunner`, optional) sees `PreCompact` before each
  pass; `block` skips the pass (strategy "blocked"), an error surfaces.
- `LLMSummarizer` is the model seam (a `Completer` — the daemon's caller);
  `Service.For(ref)` binds a run's model, `DefaultModel` overrides it
  with the cheap summarizer (`compaction.summarizer`, decision D4).
- `ErrNothingToSummarize` means "skip", never a run failure.
- Imports domain only. Tests: `compaction_test.go` (contract, token bound,
  orphan rule, folding, hooks, errors).
