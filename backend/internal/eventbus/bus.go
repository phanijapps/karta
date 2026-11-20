// Package eventbus implements domain.EventBus: an in-process, session-scoped
// publish/subscribe bus for timeline events. Publishing is non-blocking;
// each subscription owns a bounded channel with a drop-oldest overflow
// policy whose loss is observable via Subscription.OverflowCount.
package eventbus

import (
	"context"
	"errors"
	"sync"

	"zbot/backend/internal/domain"
)

// DefaultBuffer is the per-subscription channel capacity.
const DefaultBuffer = 256

// ErrClosed is returned by Publish after Close.
var ErrClosed = errors.New("eventbus: closed")

// Bus is a concurrency-safe in-process event bus. The zero value is not
// usable; use New.
type Bus struct {
	mu     sync.RWMutex
	topics map[string]map[*subscription]struct{}
	closed bool
	done   chan struct{} // closed by Close; ends every watcher goroutine
	buffer int
}

// Option configures a Bus.
type Option func(*Bus)

// WithBuffer sets the per-subscription channel capacity (default 256).
func WithBuffer(n int) Option {
	return func(b *Bus) {
		if n > 0 {
			b.buffer = n
		}
	}
}

// New creates a Bus implementing domain.EventBus.
func New(opts ...Option) *Bus {
	b := &Bus{
		topics: make(map[string]map[*subscription]struct{}),
		done:   make(chan struct{}),
		buffer: DefaultBuffer,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

var _ domain.EventBus = (*Bus)(nil)

// Publish delivers event to the subscribers of event.SessionID. It never
// blocks: a full subscription drops its oldest event (counted, observable)
// rather than stalling the publisher.
func (b *Bus) Publish(ctx context.Context, event domain.TimelineEvent) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrClosed
	}
	subs := make([]*subscription, 0, len(b.topics[event.SessionID]))
	for s := range b.topics[event.SessionID] {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, s := range subs {
		s.deliver(event)
	}
	return nil
}

// Subscribe registers a bounded subscription for sessionID, ending when ctx
// is cancelled or the bus closes.
func (b *Bus) Subscribe(ctx context.Context, sessionID string) domain.Subscription {
	s := &subscription{
		ch:    make(chan domain.TimelineEvent, b.buffer),
		topic: sessionID,
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(s.ch)
		return s
	}
	set, ok := b.topics[sessionID]
	if !ok {
		set = make(map[*subscription]struct{})
		b.topics[sessionID] = set
	}
	set[s] = struct{}{}
	b.mu.Unlock()

	go func() {
		// End on either the subscriber's context or bus Close, so a
		// background-context subscription cannot leak its watcher.
		select {
		case <-ctx.Done():
		case <-b.done:
		}
		b.unsubscribe(s)
	}()
	return s
}

// Close stops all delivery and closes every subscription channel. Publish
// thereafter returns ErrClosed.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.done) // wake every watcher goroutine
	for topic, set := range b.topics {
		for s := range set {
			s.close()
		}
		delete(b.topics, topic)
	}
}

func (b *Bus) unsubscribe(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return // Close already closed every subscription.
	}
	if set, ok := b.topics[s.topic]; ok {
		delete(set, s)
		if len(set) == 0 {
			delete(b.topics, s.topic)
		}
	}
	s.close()
}

// subscription is one consumer's bounded, drop-oldest mailbox.
type subscription struct {
	ch      chan domain.TimelineEvent
	topic   string
	mu      sync.Mutex // serializes deliver against close/drop
	overfl  int64
	closed  bool
	closeON sync.Once
}

var _ domain.Subscription = (*subscription)(nil)

func (s *subscription) Events() <-chan domain.TimelineEvent { return s.ch }

func (s *subscription) OverflowCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overfl
}

// deliver enqueues event without blocking the publisher. On a full buffer
// it drops the oldest queued event (counting it) and retries; only deliver
// sends, under s.mu, so the retry cannot block or fail.
func (s *subscription) deliver(event domain.TimelineEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- event:
		return
	default:
	}
	// Full: drop the oldest queued event and count the loss.
	select {
	case <-s.ch:
		s.overfl++
	default:
	}
	s.ch <- event // room was just freed; consumers only drain more
}

func (s *subscription) close() {
	s.closeON.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.ch)
	})
}
