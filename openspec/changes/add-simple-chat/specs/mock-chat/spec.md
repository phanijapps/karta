# Spec Delta

## REMOVED Requirements

### Requirement: Selectable sample conversations

**Reason:** Simple chat replaces fixture conversations with one live,
persistent session; there are no samples to select and no in-memory
conversation switching left to specify.
**Migration:** The chat surface now loads the single eternal transcript
via `GET /api/chat/messages`; the `simple-chat` capability owns its
behavior.

### Requirement: Deterministic mock composition

**Reason:** Replies now come from the selected provider model via the
chat exchange; the deterministic fixture reply no longer exists.
**Migration:** Send semantics (completion, bounded tool rounds, typed
failures) are specified by `simple-chat`.

### Requirement: Model and action previews are honest

**Reason:** The model selector became catalog-backed live selection in
`add-model-catalog`; with live replies, the "previews are honest"
requirement is superseded — the picker is real, replies are real, and
remaining honesty rules (unimplemented actions omitted, safe text
rendering) are re-homed into `simple-chat` scenarios.
**Migration:** Safe-text rendering and omitted-action behavior are
carried by the `simple-chat` exchange and rendering scenarios.
