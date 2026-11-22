# engine/contracttest

The reusable `domain.AgentEngine` conformance suite: the streaming
contract every engine implementation must honor — event identity and
ordering, close-exactly-once on done/error/cancel, unknown-session
Cancel idempotence, no silent failures, and goroutine hygiene.

- `Suite(t, Fixture)` runs the cases; a `Fixture` supplies per-scenario
  starters (quick/blocked/failing) plus required failure/completion
  event predicates, so engine-specific event vocabularies stay with the
  engine under test.
- Runs against fakes (engine package tests) and the real adapter
  (engine/eino tests) — never imports any engine framework itself.
- Adding an engine? Implement `domain.AgentEngine`, then instantiate
  this suite in your package's tests (rule 4.5, go-rules.md).
