# Change summary — add-chitta-distillation

## What landed

The evolution loop's write path: finished sessions from the
conversation store are distilled (LLM extraction through the provider
plane) into chitta through the trusted seam — `source: distiller`,
transcripts never persisted, re-runs idempotent, dead-lettering at 3
attempts.

- `backend/distiller/`: serial worker (selection → extraction →
  mapping → atomic Remember batch + Connects + entity upserts →
  MarkDistilled), embedded adapted prompt (overridable), strict output
  validation with attempt-failing garbage detection.
- Chitta seam: distillation markers (ChittaMeta), seam-write events
  (distiller source only — exactly one per logical op either path),
  full mutex-snapshot concurrency fix (every seam entry point guards
  the store read under System.mu; drv threaded through all helpers).
- Activity-based "finished" filter (10-minute idle; no explicit ended
  notion on the sessions table — honest v1).
- Settings → Advanced: live Distillation card (provider/model picker
  from /api/models, prompt editor); `GET/PUT /api/distillation`
  (contract-first, codegen regenerated); daemon-managed
  distillation.json (partial update: omitted fields persist; atomic
  temp+rename write; GET seeds the effective default prompt).
- zbotd: distiller wired (storeSource with Finished filter +
  ExecutionID from last message; llmCaller via ResolvedLookup +
  ChatClient); zone-rule extension in backend/AGENTS.md (plugin
  composition components may import other plugins' public surfaces).

## Review (zbot-reviewer)

All five P1s fixed:
1. Success resets attempts to 0 — no re-distillation-forever.
2. Finished-session filter (10-minute idle threshold; Finished flag
   threaded through Session + source adapter).
3. Store snapshot completed: every seam entry point (About, Recall,
   History, GetRecord, Contributions, FindEntities, UpsertEntity,
   SetRecordEmbeddings) guards under System.mu; drv threaded through
   prepareObservation/resolveMentions/resolveOrCreate/graphLane/mmr;
   concurrent-close race test added (green, -count=3).
4. backend/AGENTS.md zone text actually updated (prior edit silently
   failed); chitta AGENTS documents markers + seam-event split.
5. Frontend card uses the shared API client (typed fetch functions
   added to shared/api/client.ts).

P2s fixed: PUT partial update (read-modify-write; prompt persists when
omitted); GET seeds effective default prompt; entities moved after the
atomic batch (design-documented deviation); extraction >50% invalid →
attempt fails; frontend error reads code/message (contract shape);
atomic settings write (temp+rename); `_ = p` dead assignment; spec
header collision fixed (ADDED → MODIFIED for Distillation card).
Recorded: entity upserts do not emit ChittaToolInvoked (deliberate:
idempotent merge steps, documented in chitta AGENTS + design).

## Verification

Race suites green (fake + real LadybugDB, count=3 concurrent-close),
golangci 0 issues, frontend builds clean, contract valid + codegen
regenerated, openspec valid, 15/15 tasks.
