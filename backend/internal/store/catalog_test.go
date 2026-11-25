package store

import (
	"testing"
	"time"
)

// Catalog entries persist with their windows and restore on restart
// (zero provider traffic); rows written before windows existed hold a
// plain string list and still decode.
func TestCatalogPersistsWindowsRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	at := time.Now().UTC().Truncate(time.Second)
	models := []CatalogModel{{ID: "glm5", ContextWindow: 1048576}, {ID: "none"}}
	if err := s.SaveCatalogEntry("ollama", models, at); err != nil {
		t.Fatal(err)
	}
	entries, err := s.LoadCatalog()
	if err != nil || len(entries) != 1 {
		t.Fatalf("load: %v %+v", err, entries)
	}
	got := entries[0]
	if got.Provider != "ollama" || len(got.Models) != 2 || got.Models[0].ContextWindow != 1048576 || got.Models[1].ContextWindow != 0 {
		t.Fatalf("round trip: %+v", got)
	}
	if !got.RefreshedAt.Equal(at) {
		t.Fatalf("refreshed_at: %v vs %v", got.RefreshedAt, at)
	}
	// Upsert replaces the list.
	if err := s.SaveCatalogEntry("ollama", []CatalogModel{{ID: "only"}}, at); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.LoadCatalog()
	if len(entries) != 1 || len(entries[0].Models) != 1 || entries[0].Models[0].ID != "only" {
		t.Fatalf("upsert: %+v", entries)
	}
}

func TestCatalogLoadLegacyStringIDs(t *testing.T) {
	legacy := decodeCatalogModels(`["glm-5.3-flash","glm-5.3"]`)
	if len(legacy) != 2 || legacy[0].ID != "glm-5.3-flash" || legacy[0].ContextWindow != 0 {
		t.Fatalf("legacy decode: %+v", legacy)
	}
	modern := decodeCatalogModels(`[{"id":"a","context_window":4096},{"id":"b"}]`)
	if len(modern) != 2 || modern[0].ContextWindow != 4096 || modern[1].ID != "b" {
		t.Fatalf("modern decode: %+v", modern)
	}
	if decodeCatalogModels(`{broken`) != nil {
		t.Fatal("garbage decodes to nothing")
	}
}
