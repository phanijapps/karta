package chitta

import (
	"context"

	"zbot/backend/internal/domain"
)

// MarkDistilled records the distiller checkpoint for one session
// (trusted host op; the marker lives in chitta's metadata).
func (s *System) MarkDistilled(ctx context.Context, sessionID string, attempts int, deadLetter bool) error {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return closedErr
	}
	return drv.MarkDistilled(ctx, sessionID, attempts, deadLetter)
}

// DistillationMarkers returns every distillation checkpoint keyed by
// session id (the distiller filters against the session list).
func (s *System) DistillationMarkers(ctx context.Context) (map[string]domain.DistillationMarker, error) {
	s.mu.Lock()
	drv := s.store
	s.mu.Unlock()
	if drv == nil {
		return nil, closedErr
	}
	return drv.DistillationMarkers(ctx)
}
