package main

// The model catalog's persistence wiring: restore persisted entries on
// boot (no provider requests), then persist every future refresh so
// restarts keep the catalog warm. Extracted from main.go (LOC budget).

import (
	"log"
	"time"

	"zbot/backend/internal/providers"
	zbotstore "zbot/backend/internal/store"
)

// restoreCatalog builds the model catalog over the store's persisted
// entries (warm restarts; zero provider traffic).
func restoreCatalog(db *zbotstore.Store) *providers.Catalog {
	catalog := providers.NewCatalog()
	if entries, err := db.LoadCatalog(); err == nil {
		for _, e := range entries {
			models := make([]providers.Model, 0, len(e.ModelIDs))
			for _, id := range e.ModelIDs {
				models = append(models, providers.Model{ID: id})
			}
			catalog.LoadEntry(e.Provider, models, e.RefreshedAt)
		}
		log.Printf("zbotd: model catalog restored (%d provider(s) from last refresh)", len(entries))
	}
	catalog.OnSet(func(provider string, models []providers.Model, refreshedAt time.Time) {
		ids := make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		if err := db.SaveCatalogEntry(provider, ids, refreshedAt); err != nil {
			log.Printf("zbotd: catalog persist for %q failed: %v", provider, err)
		}
	})
	return catalog
}
