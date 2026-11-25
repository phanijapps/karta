# engine/eino

The CloudWeGo Eino adapter — the ONLY package that imports
eino/eino-ext (constitution: engine isolation). A bounded loop over
Generate + WithTools with direct control of step counting, cancellation
checks, and durable-first event emission.

- `Adapter` implements `domain.AgentEngine` (RunGoal/Cancel; Resume is
  declared not-yet — the contract suite pins that deferral).
- `newModel` is the unexported model-factory seam: tests inject fake
  generators (no live provider); production builds the OpenAI-compatible
  client. The models cache keys on provider/model/token-hash.
- Known, pinned bugs (characterization tests in adapter_test.go flip
  when the run-ownership follow-up lands): caller ctx is detached after
  RunGoal returns (F-23); same-session overlap orphans the first run's
  cancel handle (F-24); the models-cache key omits BaseURL (F-26).
- Imports engine + domain + providers + store + tools + eventbus;
  nothing else may import eino.

- `Adapter.DropWard(sessionID)` resets the session's ward host on
  leave-ward so the next run starts unhosted (wired via the server's
  `SetWardDropper`). Tool-call bus events carry `Text` = tool name
  (every SSE consumer renders it).

- Ward events (add-ward-hosting): the per-session host's transition
  observer → `emitWardEvent` persists `ward_entered`/`ward_created`
  with the `WardEventPayload` then publishes (the F-15 posture).
  `delegateTool` composes the `## Active Ward` section onto the
  child's prompt and appends the ward guard to its description while
  hosted.
