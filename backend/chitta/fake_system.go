package chitta

import "zbot/backend/internal/domain"

// newFakeSystem builds the fake-driver-backed System used by
// OpenInMemory (tests/tooling).
func newFakeSystem(bus domain.EventBus) *System {
	return &System{store: newFakeDriver(), bus: bus}
}
