package domain

import "context"

// EventBus publishes session-scoped timeline events in-process. The
// implementation lives in internal/eventbus/; consumers (engines, hooks,
// the SSE server, future persistence) depend only on this seam.
type EventBus interface {
	// Publish delivers event to the subscribers of event.SessionID's topic.
	// It never blocks on slow subscribers and succeeds on topics with no
	// subscribers.
	Publish(ctx context.Context, event TimelineEvent) error
	// Subscribe registers a bounded subscription for one session topic,
	// ending when ctx is cancelled or the bus closes.
	Subscribe(ctx context.Context, sessionID string) Subscription
}

// Subscription is one consumer's handle on a session topic.
type Subscription interface {
	// Events is the delivery channel; it closes when the subscription ends.
	Events() <-chan TimelineEvent
	// OverflowCount reports how many events were dropped from this
	// subscription's buffer (drop-oldest policy) so loss is observable.
	OverflowCount() int64
}
