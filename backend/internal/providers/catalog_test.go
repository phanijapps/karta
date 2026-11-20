package providers

import (
	"sync"
	"testing"
)

func TestCatalogEmptyStart(t *testing.T) {
	snap := NewCatalog().Snapshot()
	if len(snap.Models) != 0 || len(snap.RefreshedAt) != 0 {
		t.Fatalf("fresh catalog must be empty: %+v", snap)
	}
}

func TestCatalogSetGetSnapshot(t *testing.T) {
	c := NewCatalog()
	c.Set("ollama", []Model{{ID: "b"}, {ID: "a"}})
	c.Set("zai", []Model{{ID: "glm"}})

	if models, _, ok := c.Get("ollama"); !ok || len(models) != 2 {
		t.Fatalf("Get: %+v ok=%v", models, ok)
	}
	if _, _, ok := c.Get("never"); ok {
		t.Fatal("unrefreshed provider must report ok=false")
	}

	snap := c.Snapshot()
	// Stable order: provider name first, then the provider's list order.
	want := []CatalogModel{{"ollama", "b"}, {"ollama", "a"}, {"zai", "glm"}}
	if len(snap.Models) != 3 {
		t.Fatalf("want 3 entries, got %+v", snap.Models)
	}
	for i, w := range want {
		if snap.Models[i] != w {
			t.Fatalf("order: got %+v want %+v", snap.Models, want)
		}
	}
	if len(snap.RefreshedAt) != 2 || snap.RefreshedAt["zai"].IsZero() {
		t.Fatalf("timestamps: %+v", snap.RefreshedAt)
	}

	// A later Set replaces the entry (no accumulation).
	c.Set("ollama", []Model{{ID: "only"}})
	if models, _, _ := c.Get("ollama"); len(models) != 1 || models[0].ID != "only" {
		t.Fatalf("replacement: %+v", models)
	}
}

func TestCatalogConcurrentAccess(t *testing.T) {
	c := NewCatalog()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); c.Set("p", []Model{{ID: "m"}}) }(i)
		go func() { defer wg.Done(); _ = c.Snapshot() }()
	}
	wg.Wait()
}
