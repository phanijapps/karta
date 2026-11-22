package chitta

import (
	"zbot/backend/internal/domain"

	// The cgo LadybugDB binding is confined to this subtree (boundary
	// grep: go-ladybug is imported only here and in internal/ladybug).
	"zbot/backend/chitta/internal/ladybug"
)

// Options configures Open.
type Options struct {
	// Bus is the optional eventbus; chitta events are best-effort.
	Bus domain.EventBus
	// EmbeddingDim fixes the vector dimension at database creation
	// (default 384). A later open with a different dimension fails with
	// a typed unavailable error — never a silent re-index.
	EmbeddingDim int
}

// Open opens (creating if needed) the chitta database at dir and returns
// the System implementing both seams (memory + knowledge). The daemon's
// composition root calls this per the `chitta:` config section.
func Open(dir string, opts Options) (*System, error) {
	drv, err := ladybug.Open(dir, opts.EmbeddingDim)
	if err != nil {
		return nil, err
	}
	return &System{store: drv, bus: opts.Bus}, nil
}

// OpenInMemory builds a System over the in-process fake driver —
// test/tooling surface (no cgo, no disk). Production uses Open.
func OpenInMemory(bus domain.EventBus) (*System, error) {
	return newFakeSystem(bus), nil
}
