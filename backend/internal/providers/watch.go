package providers

// The provider directory watcher: fsnotify events debounce into
// Reload (under the store mutex, so a reload can never publish a stale
// set over a completed mutation). Extracted from store.go (LOC budget).

import (
	"log"
	"time"
)

// providerWatchDebounce coalesces editor save bursts (registry parity).
const providerWatchDebounce = 150 * time.Millisecond

// watchLoop reloads on provider-directory changes, debounced like the
// registry watcher. Reload runs under the store mutex, so it can never
// publish a stale set over a completed mutation.
func (s *Store) watchLoop() {
	var timer *time.Timer
	for {
		select {
		case <-s.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case _, ok := <-s.fw.Events:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(providerWatchDebounce, func() {
				// Errors are logged inside Reload; last good set keeps serving.
				_ = s.Reload()
			})
		case err, ok := <-s.fw.Errors:
			if !ok {
				return
			}
			log.Printf("providers: watcher error: %v", err)
		}
	}
}
