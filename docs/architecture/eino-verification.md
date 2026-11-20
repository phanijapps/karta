# Eino verification & conversation-persistence decision

Verified 2026-11-20 by a hands-on spike (`/tmp/eino-spike`, throwaway
module — no repo dependencies changed) plus source inspection. Informs
`openspec/changes/add-simple-chat/`.

## Eino verification results

Versions: `github.com/cloudwego/eino v0.9.23` (Apache-2.0, 13.7k stars,
pushed within the last day) + `eino-ext/components/model/openai v0.1.13`.
Spike ran against the local ollama's OpenAI-compatible endpoint, final
round via a fast cloud model.

**Verified working, end to end (2.6s round trip):**

1. **OpenAI-compatible model with custom base URL** —
   `openai.NewChatModel(ctx, &openai.ChatModelConfig{BaseURL,
   APIKey, Model})` pointed at ollama; plain completion returned
   correctly. This is exactly the seam our provider plane resolves
   (base URL + keyring/env credential + catalog model id).
2. **Tool calling, full round** — `schema.ToolInfo{Name, Desc,
   ParamsOneOf}` offered via `model.WithTools`; the model returned a
   structured `ToolCall{ID, Function{Name, Arguments}}`; the tool result
   went back as `schema.Message{Role: schema.Tool, ToolCallID, Content}`;
   the final answer used the executed tool's real value.
3. **Token usage metadata** — `out.ResponseMeta.Usage` carries
   `PromptTokens` / `CompletionTokens` / `TotalTokens` per turn.
4. `schema.Message` shape: `Role, Content, ToolCalls[], ToolCallID,
   ToolName, ResponseMeta, ReasoningContent, Name` + multimodal parts.

**Verified absences (important):**

- **Eino v0.9.x has no ChatHistory/memory component.**
  `Model.Generate(ctx, []schema.Message)` is stateless; conversation
  history is the caller's job. There is nothing in eino that stores,
  rotates, or manages conversations.

Notes: local 32B models can exceed 60s per turn (spike timeouts on
`qwen3:32b`) — request deadlines must be generous and configurable.
`eino-ext` pins a community go-openai fork (`meguminnnnnnnnn/go-openai`)
— acceptable, but the isolation rule (imports only under
`internal/engine/eino/`) keeps that contained.

**Verdict: Eino fits the plan as constitutionally placed** — behind
`domain.AgentEngine`, with our own store owning conversations. The
`add-simple-chat` slice does not need eino at all (the providers client
suffices); the engine change later wraps the verified
`ChatModelConfig`/tool-call mechanics.

## Best way to save conversations (decision, now evidence-backed)

**Append-only SQLite `messages` table, one row per conversation turn,
mapping 1:1 to eino's `schema.Message`.** Grounded three ways:

1. **Eino requires caller-owned history** (verified absence above) —
   something must store and replay `[]schema.Message`; there is no
   framework shortcut to defer to.
2. **agentzero precedent** (`docs/inspiration/agentzero.md`) — the same
   shape survived production there: role/content/timestamps, tool_calls
   JSON, tool_call_id link, and atomic per-session `seq` for cursor
   replay.
3. **Our constitution** — SQLite at `~/.zbot/data/zbot.db`, local-first,
   timeline-observable.

Rejected alternatives: JSONL files (no concurrent-append safety, no
cursor pagination without full scans, no row-level events), single-blob
session JSON (unbounded rewrite cost as the eternal transcript grows),
and deferring to eino memory (does not exist).

**Refinements to the `add-simple-chat` store schema from verification:**

- Add `tool_name` (eino carries it alongside `tool_call_id`).
- Persist per-message token usage from `ResponseMeta.Usage`
  (`prompt_tokens`, `completion_tokens`) — this is what makes future
  compaction budgets computable without re-reading anything.
- The future engine adapter is a trivial projection:
  `Replay() → []*schema.Message` (fields align exactly).
