# Findings — intent-context-packet

## Phase gates

### Gate 1 — gpt-6.1-terra (backend: packet analyzer, state, /v1)

(pending)

### Gate 2 — gpt-6.1-sol (frontend + UX)

(pending)

## Decisions (recorded during implementation)

- **D1 — Text-JSON, no response_format ladder.** We never adopted
  `response_format`; agentzero's mode ladder solves a failure mode we
  do not have. Fence-stripping (extractJSON) already covers wrapped
  JSON. (design.md decision 3.)
- **D2 — Last-parsed fallback on validation residue.** When every
  correction retry still names unknown resources, the LAST PARSED
  decision returns and the wrapper's Sanitize drops the residue — a
  good analysis is not wasted on one phantom ID (preserves the old
  drop-and-warn behavior as the belt-and-braces path).
- **D3 — Empty sets are authoritative.** Packet validation treats an
  empty roster as "nothing valid" (not "skip validation") — an empty
  MCP roster means no MCP recommendations are valid. Ward use_existing
  against an unwired vault correctly fails validation.
- **D4 — Ranking from the existing RecallResources seam.** The recall's
  per-kind hit order ranks the packet; no new chitta rank API was
  needed (host roster from the registry snapshot + ward vault).
- **D5 — /v1 model resolution.** The OpenAI `model` field selects the
  session TYPE; the actual LLM is the daemon's configured default
  model (the operator's picker). OpenWebUI has no idea which provider
  Karta uses — the daemon decides.
- **D6 — Non-streaming /v1 in v1.** `stream` is accepted and ignored;
  OpenWebUI tolerates non-streaming completions. Streaming chunks are
  a follow-up.
- **D7 — Task 3.3 (final-answer persistence): investigated, NO BUG.**
  The NVDA run's report IS the transcript's final row (2790 chars);
  the post-mortem misread a truncated query. The perceived "nothing
  showed" was the stuck draft + the dead analysis card — both fixed
  here. Recorded, no code change.

## P2s (recorded, not blocking)

- **P2-1 — /v1 streaming.** OpenAI chunked SSE for chat/completions
  (OpenWebUI shows progressive typing). Follow-up.
- **P2-2 — First-message-hash collision.** Two OpenWebUI conversations
  with identical first messages share a session (same human, same
  opening — accepted; design.md risk).
- **P2-3 — OpenWebUI conversation renames don't propagate** (the
  session keeps the hash id + "Research" title). A later
  session-rename surface covers it.

## Final review

(pending — zbot-reviewer + task verify)
