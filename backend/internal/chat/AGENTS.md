# chat

The LEAN chat exchange (the session model, add-chat-modes): one
serialized send per CHAT-type session that appends the user message,
drives a bounded completion (with built-in tools) against the selected
provider model, persists every row (intent-before-execution), and
emits durable-first timeline events. RESEARCH sessions never run this
exchange — `SendIn`/`SendStreamIn` route them to the wired research
path (the engine pipeline; daemon wiring in cmd/zbotd/researchpath.go)
before the lean path begins. No agents here — the base exchange the
engine layer generalizes.

- Routing is SESSION-scoped: `SendIn`/`SendStreamIn` resolve the
  session row (`resolveSession` — the default quick chat is created on
  first use, unknown ids fail `ErrUnknownSession` → 404) and route by
  its immutable type. There is no mode field anywhere.
- Lean composition: persona (daemon-composed, custom sections dropped)
  + optional `## Context` memory recall (`SetContext` seam — the type
  gates the pipeline, never the knowledge).
- `Service.SendIn` is serialized per service (sendMu); no database
  transaction spans a provider call.
- `Completer` is the seam over the providers chat client (tests inject
  scripted completions). Tool rounds capped at 2; the final round offers
  no tools (forces an answer).
- Failure semantics: the user row and every committed intermediate row
  persist; no final-answer row on failure; the typed provider failure is
  returned alongside the persisted user row and cursor.
- `SyncIn` pages one session's transcript for clients (page cursor =
  last delivered seq, never the database tail).
- Imports store + providers + tools + eventbus + domain; never imports
  the server or any client code.
