package eventbus

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zbot/backend/internal/domain"
)

func ev(sessionID string, i int) domain.TimelineEvent {
	return domain.TimelineEvent{ID: string(rune('a'+i%26)) + time.Now().Format("150405.000000000"), SessionID: sessionID, Kind: domain.EventAgentStep}
}

func TestTopicIsolation(t *testing.T) {
	bus := New()
	t.Cleanup(func() { bus.Close() })
	ctx := context.Background()
	sa := bus.Subscribe(ctx, "A")
	sb := bus.Subscribe(ctx, "B")

	for i := 0; i < 3; i++ {
		if err := bus.Publish(ctx, ev("A", i)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case <-sb.Events():
			t.Fatal("subscriber B received an A event")
		case <-time.After(20 * time.Millisecond):
		}
	}
	got := 0
	for i := 0; i < 3; i++ {
		<-sa.Events()
		got++
	}
	if got != 3 {
		t.Fatalf("subscriber A got %d events, want 3", got)
	}
}

func TestPublishWithoutSubscribers(t *testing.T) {
	bus := New()
	t.Cleanup(func() { bus.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := bus.Publish(context.Background(), ev("nobody", 0)); err != nil {
			t.Errorf("Publish with no subscribers: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish with no subscribers blocked")
	}
}

func TestSlowSubscriberDoesNotStallPublisher(t *testing.T) {
	bus := New(WithBuffer(2))
	t.Cleanup(func() { bus.Close() })
	ctx := context.Background()
	_ = bus.Subscribe(ctx, "A") // never consumed

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			if err := bus.Publish(ctx, ev("A", i)); err != nil {
				t.Errorf("Publish: %v", err)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher stalled on a full subscription")
	}
}

func TestSubscriberIsolationWithObservableOverflow(t *testing.T) {
	bus := New(WithBuffer(2))
	t.Cleanup(func() { bus.Close() })
	ctx := context.Background()
	slow := bus.Subscribe(ctx, "A")
	fast := bus.Subscribe(ctx, "A")

	const total = 10
	// Drain "fast" concurrently with publishing: it must receive every
	// event while the never-drained subscription overflows.
	fastDone := make(chan int, 1)
	go func() {
		got := 0
		for got < total {
			select {
			case <-fast.Events():
				got++
			case <-time.After(2 * time.Second):
				fastDone <- got
				return
			}
		}
		fastDone <- got
	}()

	for i := 0; i < total; i++ {
		if err := bus.Publish(ctx, ev("A", i)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		// Pace publishes so a promptly-draining subscriber keeps up (a real
		// SSE bridge drains far faster than engine steps arrive); the slow
		// subscription still never drains.
		time.Sleep(5 * time.Millisecond)
	}

	if got := <-fastDone; got != total {
		t.Fatalf("fast subscriber received only %d of %d", got, total)
	}

	// The never-drained subscription overflowed exactly total-buffer events
	// (drop-oldest, buffer 2) and the loss is observable.
	if c := slow.OverflowCount(); c != total-2 {
		t.Fatalf("overflow count = %d, want exactly %d (drop-oldest, buffer 2)", c, total-2)
	}
}

func TestContextCancellationClosesSubscription(t *testing.T) {
	bus := New()
	t.Cleanup(func() { bus.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	s := bus.Subscribe(ctx, "A")

	if err := bus.Publish(ctx, ev("A", 0)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	cancel()

	select {
	case _, open := <-s.Events():
		if open {
			// Draining the queued event first is allowed; the channel must
			// then be closed.
			_, open2 := <-s.Events()
			if open2 {
				t.Fatal("subscription channel still open after cancellation")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("subscription channel did not close after cancellation")
	}
}

func TestCloseStopsAllDelivery(t *testing.T) {
	bus := New()
	t.Cleanup(func() { bus.Close() })
	// Baseline captured here (not TestMain) so framework goroutines started
	// after TestMain are included.
	before := runtime.NumGoroutine()
	ctx := context.Background() // deliberately non-cancellable: Close must end these
	sa := bus.Subscribe(ctx, "A")
	sb := bus.Subscribe(ctx, "B")

	bus.Close()

	for _, s := range []domain.Subscription{sa, sb} {
		if _, open := <-s.Events(); open {
			t.Fatal("channel still open after Close")
		}
	}
	if err := bus.Publish(ctx, ev("A", 0)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish after Close: got %v, want ErrClosed", err)
	}

	// Goroutine-leak check: every watcher goroutine must exit after Close.
	// Two Subscribe calls spawned two watchers; poll until the count returns
	// to the pre-subscribe baseline.
	leakCheck(t, func() bool {
		return runtime.NumGoroutine() <= before
	})
}

func leakCheck(t *testing.T, settled func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !settled() {
		if time.Now().After(deadline) {
			t.Fatal("goroutine count did not return to baseline - leaked goroutines")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConcurrentPublishSubscribeRaceClean(t *testing.T) {
	bus := New(WithBuffer(64))
	t.Cleanup(func() { bus.Close() })
	ctx := context.Background()

	var received int64
	subs := make([]domain.Subscription, 4)
	for i := range subs {
		subs[i] = bus.Subscribe(ctx, "race")
	}
	for _, s := range subs {
		s := s
		go func() {
			for range s.Events() {
				atomic.AddInt64(&received, 1)
			}
		}()
	}

	var wg sync.WaitGroup
	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if err := bus.Publish(ctx, ev("race", i)); err != nil {
					t.Errorf("Publish: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Give drainers a moment, then close and count.
	time.Sleep(200 * time.Millisecond)
	bus.Close()

	minExpected := int64(8 * 200 / 4 / 4) // generous lower bound per subscriber
	if atomic.LoadInt64(&received) < minExpected {
		t.Fatalf("suspiciously few deliveries: %d", received)
	}
}
