package providers

import (
	"sort"
	"sync"
	"time"
)

// Catalog is the daemon's in-memory model catalog: per-provider model
// lists with their last successful refresh time. Nothing is persisted
// under ~/.zbot/ and nothing survives a restart — manual-refresh
// semantics by design. Reads are instant snapshot reads; all warming
// goes through Set (the refresh endpoint and successful discovery share
// that one path).
type Catalog struct {
	mu      sync.Mutex
	entries map[string]catalogEntry
}

type catalogEntry struct {
	models      []Model
	refreshedAt time.Time
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{entries: map[string]catalogEntry{}}
}

// Set replaces one provider's catalog entry and stamps the refresh time.
func (c *Catalog) Set(provider string, models []Model) {
	list := make([]Model, len(models))
	copy(list, models)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[provider] = catalogEntry{models: list, refreshedAt: time.Now().UTC()}
}

// Get returns one provider's entry (ok=false when never refreshed).
func (c *Catalog) Get(provider string) ([]Model, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[provider]
	if !ok {
		return nil, time.Time{}, false
	}
	list := make([]Model, len(e.models))
	copy(list, e.models)
	return list, e.refreshedAt, true
}

// CatalogSnapshot is an immutable read of the whole catalog: flat
// provider+id entries in stable order (provider name, then list order)
// plus each provider's refresh timestamp.
type CatalogSnapshot struct {
	Models      []CatalogModel
	RefreshedAt map[string]time.Time
}

// CatalogModel is one flat catalog entry.
type CatalogModel struct {
	Provider string
	ID       string
}

// Snapshot reads the whole catalog without dialing anything.
func (c *Catalog) Snapshot() CatalogSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := CatalogSnapshot{Models: []CatalogModel{}, RefreshedAt: map[string]time.Time{}}
	names := make([]string, 0, len(c.entries))
	for name := range c.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := c.entries[name]
		snap.RefreshedAt[name] = e.refreshedAt
		for _, m := range e.models {
			snap.Models = append(snap.Models, CatalogModel{Provider: name, ID: m.ID})
		}
	}
	return snap
}
