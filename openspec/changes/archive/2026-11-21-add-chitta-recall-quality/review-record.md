# Chitta review record — 3 rounds, mixed GPT/GLM (final)

Rounds per owner instruction: three adversarial review rounds over the
whole chitta capability (implementation + specs), using a combination
of GPT and GLM models, hunting dead code, complexity, and problem-fit.

## Round 1 — gpt-6.1-sol (xhigh) + glm-5.3 (xhigh): needs-fixes ×2

12 P1s: pagination infinite-loop, evidence identity not scoped by
subject/kind, unlocked logical RMW, entity resolution gaps,
SearchEntities divergence, Neighborhood column collisions, statement
leaks, swallowed errors, runtime extension installs, EntityIDs no-op,
no composition wiring. Dead code: DataSubdir, stray cursors,
DebugQuery, dataDir, hand-rolled sqrt, placeholders. **All fixed**;
regression tests added (concurrent identical evidence, sparse
pagination walk, resolve-by-id/alias).

## Round 2 — gpt-6.1-terra (high) + gpt-6.1-sol (xhigh): needs-fixes ×2

Transactional gaps (record/contribution/supersede non-atomic),
relationship-persistence regression from round 1, keyword lane
non-advancing cursor, alias-equality-not-prefix, recall error
swallowing, float-order nondeterminism, replay-after-chain mis-answer,
dossier oldest-first, three divergent normalizers. **All fixed**:
engine transactions (withTx), composite ops, UpdateRelationshipConfidence
(+triple-connect test), descending keyset cursors, shared
internal/textutil normalizer (domain stays stdlib-pure), fixed lane
scoring order, ledger-based replay, NewestFirst.

## Round 3 — glm-5.3 (xhigh) + gpt-6.1-terra (high): SHIP (8.5/10, glm) / needs-fixes (terra)

glm: ship with 3 P2s (replay error mask, fake token-overlap fallback,
undocumented dead Entity.embedding column). terra: 4 P1s (descending
tie predicates inverted; merge links duplicate; batch commits
per-item; replay errors masked) + 3 P2s (search not on
normalized_content; misleading schema comments; deferral tracking).
**All fixed**: tie predicates corrected (+equal-timestamp walk test),
deduplicated link union in-x (+test), `ApplyObservations` commits the
whole remember batch in ONE engine transaction (+injection test),
replay propagates storage errors and names ledger-integrity
violations, keyword lane searches `normalized_content` (+Unicode/
whitespace recall test), schema comments truthful (+inline-comment-
safe parser), fake mirrors production exactly (token-overlap dropped),
explicit deferral register added.

## Final verification

- `go test -race` green (all packages; pre-existing skillhub WIP excluded)
- `CHITTA_DRIVER=ladybug go test -race ./chitta/` green — same suite
  plus E2E: restart persistence, dimension mismatch, data-dir
  isolation, 120-row sparse pagination, equal-timestamp pagination,
  merge-link dedup, normalized recall, concurrency
- golangci-lint: 0 issues; domain purity green; openspec valid

## Verdict

glm-5.3 (xhigh): **SHIP** — "Chitta solves zbot's memory/knowledge
problem for v1… nothing is silently missing." terra's blocking items
were fixed in the final pass with tests; the residual state is the
post-fix tree verified above.


## Final constitution gate (zbot-reviewer)

Verdict NEEDS-FIXES → all five P1s fixed and re-verified:

1. `chitta.recall.*` validation (ranges for weights/lambda/halflife/
   rerank_top) + per-knob defaults in ConfigureRecall (partial overrides
   no longer zero other lanes) + config table tests.
2. Missing scenario tests added: MMR near-duplicate interleaving,
   recency tie-break, weights-are-config, async producer engagement,
   (BM25 stemming asserted via the harness on the real driver;
   cold-start covered by vendored loading + no-INSTALL runtime path).
3. Eval harness now engages vector + MMR lanes (deterministic 384-dim
   content embedder, near-duplicate fixtures, backfill via the trusted
   seam); floors recalibrated to measured truth (nDCG@10 0.829,
   MRR@10 0.762 over floors 0.7/0.75) — no more BM25-only calibration.
4. Clean-diff purge: duplicate initSchema call, OpenRaw, zero-assertion
   probe test, stacked/stale docstrings (SearchRecords/store.go/fake),
   dead MMR conditional, go.mod indirect drift (tidy), DebugQuery
   comment truthed.
5. Local-first: cold model cache keeps the producer OFF (daemon never
   downloads); zbotd logs degraded mode.

P2s fixed or recorded: migration copy switched to bound parameters
(no literal Cypher); extension dir override (`ladybug.ExtensionDir`) +
launch-cwd documented in AGENTS.md; ALGO vendoring explicitly deferred
in spec + tasks; harness nits (strings.Contains, substring semantics
documented, sabotage-floor via a corrupted query).

Final verification: fake + real-driver race suites green (incl. MMR/
producer/vector scenarios), eval harness green over floors, golangci
0 issues, builds clean, contract valid, openspec valid.
