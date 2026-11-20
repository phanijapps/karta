# eventbus

In-process, session-scoped publish/subscribe for `domain.TimelineEvent`.
Implements `domain.EventBus`; the only import is `internal/domain`.

- Publish is non-blocking: each subscription owns a bounded channel
  (default 256, `WithBuffer`) with a drop-oldest overflow policy; loss is
  observable via `Subscription.OverflowCount()`.
- Subscriptions end on context cancellation or `Bus.Close()` — watchers exit
  on either path, so background-context subscriptions cannot leak
  (covered by the goroutine-leak test).
- This package transports events only: no persistence, no SSE, no policy.
  The Timeline Tree store and SSE bridge subscribe through the
  `domain.EventBus` seam later.
