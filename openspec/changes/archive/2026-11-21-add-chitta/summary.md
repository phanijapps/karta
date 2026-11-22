# Change summary — add-chitta

## What landed

Chitta (चित्त, the mind-field): the core the harness uses to
continuously evolve. One capability, two first-class domains — memory
(experiential records about the user) and knowledge (the entity graph of
the user's world) — in ONE LadybugDB database at `~/.zbot/data/chitta/`,
behind kernel seams (`domain.MemoryStore` / `domain.KnowledgeGraph`),
exposed through seven transport-neutral tools
(`memory_remember/recall/revise/history`,
`knowledge_connect/find_entities/about`) with evidence-aware writes
(contribution idempotency, converging confidence, no resurrection of
corrections, kind-inheriting revisions, no deletes) and one
`ChittaToolInvoked` timeline event per call (contract-first: enum +
payload landed in `contracts/openapi.yaml` before event code).

Implementation: `backend/chitta/**` (service + tools + events +
`internal/ladybug` driver, go-ladybug vendored at
`third_party/go-ladybug` via go.work + replace, CGO_ENABLED=1),
`backend/internal/domain/memory.go` (contracts),
`backend/internal/domain/event.go` (+kind/payload), `chitta:` config
section (loud unknown-provider failure), constitutions amended (root
Global Rule 1, backend kernel/plugin zones), product-context updated,
Taskfile (`test:chitta:ladybug`, verify includes the real-driver suite).

## Review (zbot-reviewer, read-only)

All five P1 findings fixed:
1. merge-with-links CREATE-on-existing → new `UpdateRecordLinks` driver op.
2. unsynchronized reads on the single cgo connection → global lock in
   `exec`/`queryRows`.
3. entity-id collisions from ASCII-stripping → sha256-hash ids.
4. content-mention auto-linking implemented (+ strengthened test).
5. post-fetch filtering stranding pagination → loop-fetch to `limit`
   survivors (walk exact on both drivers).

P2s fixed: affected_ids never null; SearchRecords respects limit;
entity-search aligned top-k across drivers (spec amended — recall/find
are top-k, cursor is a contract-revision follow-up); dossier
subject-records DESC; stale comments/junk removed; Entity embedding
column added (D5 parity); `go mod tidy`; sqlite-vec residue cleared from
backend/AGENTS.md header + agent roster; verify includes
`test:chitta:ladybug`; whitespace-variant merge fixed via stored
`normalized_content` (+ cross-driver test); entity-resolution errors now
fail the write instead of silently unlinking.

## P2s deferred (with reasons)

- **Storage-atomic batches**: `Remember`/`Connect` are all-or-nothing on
  validation, but a mid-batch driver error leaves earlier items written;
  the go-ladybug binding does not expose multi-statement transactions.
  Follow-up: distiller change (which needs exactly this) or driver-level
  transaction support.
- **Full Unicode NFC/casefold normalization**: v1 uses ToLower +
  whitespace collapse, applied identically on both sides of every
  comparison (service and stored `normalized_content`); NFC folding is a
  follow-up with the embedding/recall-quality work.
- **Entity embeddings**: column declared; producer follow-up is decided —
  fastembed-go with `BAAI/bge-small-en-v1.5` (384-dim, matches the
  shipped default).
- **`Mentions` rel table**: declared for future graph-side traversals;
  record `entities` array membership is the normative link in v1.
- **Contract ordering evidence**: the 1.3-before-2.4 (contract before
  event code) ordering is recorded in tasks.md; the repo has no commits
  for this change yet, so git history cannot corroborate.

## Verification

- `go test -race` green for all packages except `internal/skillhub` —
  **pre-existing untracked WIP from the separate skill-marketplace
  change**, failing before this change touched anything; out of scope.
- `CHITTA_DRIVER=ladybug go test -race ./chitta/` green (same suite on
  the real driver + E2E: restart persistence, dimension-mismatch,
  data-dir isolation, concurrency).
- golangci-lint: 0 issues; go vet clean; `go build ./...` clean.
- Codegen: redocly valid; TS client zero drift; Go codegen stable.
- Frontend: svelte-check 0 errors (1 pre-existing warning), build clean.
- Boundary greps: go-ladybug only in `chitta/internal/ladybug`; no
  kernel package imports chitta; chitta imports only kernel; contract
  enum synced across yaml/Go/TS.
