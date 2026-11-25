// The ROOT run wrapper (extracted from adapter.go, LOC budget): the
// goroutine RunGoal starts — cleanup, cancellation registration, channel
// close — around the shared runLoop. Children bypass it.

package eino

import (
	"context"

	"zbot/backend/internal/domain"
)

// run is the ROOT wrapper: cleanup, cancellation registration, channel
// close. Children bypass it (no cancels-map entry, no channel close).
func (a *Adapter) run(ctx context.Context, cancel context.CancelFunc, events chan domain.TimelineEvent, in runInput) {
	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.cancels, in.sessionID)
		delete(a.runAttr, in.sessionID) // the run is over: no attribution to keep
		a.mu.Unlock()
		close(events)
	}()
	// Root run: the answer was already emitted as the final agent step;
	// runLoop's error variants emit their own failure events (the caller
	// observes the stream, not the return value).
	if _, err := a.runLoop(ctx, events, in); err != nil {
		// Nothing further to do at the root: the failure event and
		// execution status were already recorded inside the loop.
		_ = err
	}
}
