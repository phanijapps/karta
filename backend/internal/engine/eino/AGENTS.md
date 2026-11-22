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
