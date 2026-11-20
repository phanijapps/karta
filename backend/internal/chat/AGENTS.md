# chat

The simple chat exchange: one serialized send per conversation that
appends the user message, drives a bounded completion (with built-in
tools) against the selected provider model, persists every row
(intent-before-execution), and emits durable-first timeline events.
No agents, no streaming — the base exchange the engine layer later
generalizes.

- `Service.Send` is serialized per session (sendMu); no database
  transaction spans a provider call.
- `Completer` is the seam over the providers chat client (tests inject
  scripted completions). Tool rounds capped at 2; the final round offers
  no tools (forces an answer).
- Failure semantics: the user row and every committed intermediate row
  persist; no final-answer row on failure; the typed provider failure is
  returned alongside the persisted user row and cursor.
- `Sync` pages the transcript for clients (page cursor = last delivered
  seq, never the database tail).
- Imports store + providers + tools + eventbus + domain; never imports
  the server or any client code.
