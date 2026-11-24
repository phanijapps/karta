package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"zbot/backend/internal/store"

	"zbot/backend/internal/wards"
)

// newWardsServer builds a daemon over a real (temp) ward vault with two
// wards: amdfinancials (clean research) and quant-strategy (violations:
// missing canonical + an unexpected stray file). A real vault is the
// honest fixture — the handlers' only job is mapping wards types to the
// contract.
func newWardsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wards")
	if err := wards.Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("vault bootstrap: %v", err)
	}
	clean := filepath.Join(root, "amdfinancials")
	writeVaultFiles(t, clean, map[string]string{
		"amdfinancials.md": "# AMD financials\nsynthesis\n",
		"AGENTS.md":        "readme\n",
		"log.md":           "log\n",
		"ward-conf.yaml":   researchConfFixture,
	})
	dirty := filepath.Join(root, "quant-strategy")
	writeVaultFiles(t, dirty, map[string]string{
		"AGENTS.md":      "readme\n",
		"log.md":         "log\n",
		"ward-conf.yaml": researchConfFixture,
		"stray.txt":      "stray\n",
	})
	vault, err := wards.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("vault open: %v", err)
	}
	srv := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, vault, "test")
	return httptest.NewServer(srv.Routes()), root
}

// researchConfFixture mirrors the seeded research archetype conf.
const researchConfFixture = `
archetype: research
layout:
  kind: directory
  children:
    - { id: canonical, match: "{ward}.md", kind: file, format: markdown }
    - { id: agent-instructions, match: AGENTS.md, kind: file, format: markdown }
    - { id: log, match: log.md, kind: file, format: markdown }
    - id: tasks
      match: tasks
      kind: directory
      required: false
      children:
        - { id: task, match: "*.md", kind: file, required: false, repeat: true, format: markdown }
    - { id: pages, match: pages, kind: directory, required: false }
`

func writeVaultFiles(t *testing.T, dir string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for rel, content := range entries {
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func TestWardsListCarriesLintStatesAndVaultRoot(t *testing.T) {
	t.Parallel()
	daemon, root := newWardsServer(t)
	defer daemon.Close()

	res, err := http.Get(daemon.URL + "/api/wards")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status: %d", res.StatusCode)
	}
	var list struct {
		VaultRoot string `json:"vault_root"`
		Wards     []struct {
			Name           string  `json:"name"`
			Archetype      string  `json:"archetype"`
			LintStatus     string  `json:"lint_status"`
			FindingCount   int     `json:"finding_count"`
			CanonicalTitle *string `json:"canonical_title"`
		} `json:"wards"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.VaultRoot != root {
		t.Fatalf("vault_root: %q want %q", list.VaultRoot, root)
	}
	if len(list.Wards) != 2 {
		t.Fatalf("want 2 wards, got %+v", list.Wards)
	}
	clean, dirty := list.Wards[0], list.Wards[1]
	if clean.Name != "amdfinancials" || clean.Archetype != "research" || clean.LintStatus != "clean" || clean.FindingCount != 0 {
		t.Fatalf("clean ward summary: %+v", clean)
	}
	if clean.CanonicalTitle == nil || *clean.CanonicalTitle != "AMD financials" {
		t.Fatalf("canonical title: %+v", clean.CanonicalTitle)
	}
	if dirty.Name != "quant-strategy" || dirty.Archetype != "research" || dirty.LintStatus != "violations" || dirty.FindingCount != 2 {
		t.Fatalf("dirty ward summary: %+v", dirty)
	}
	if dirty.CanonicalTitle != nil {
		t.Fatalf("ward without canonical page must have no title: %+v", dirty)
	}
}

func TestWardsDetailTreeShape(t *testing.T) {
	t.Parallel()
	daemon, _ := newWardsServer(t)
	defer daemon.Close()

	res, err := http.Get(daemon.URL + "/api/wards/amdfinancials")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("detail status: %d", res.StatusCode)
	}
	var ward struct {
		Name      string `json:"name"`
		Archetype string `json:"archetype"`
		Layout    *struct {
			Kind     string `json:"kind"`
			Children []struct {
				ID    string `json:"id"`
				Kind  string `json:"kind"`
				Match string `json:"match"`
			} `json:"children"`
		} `json:"layout"`
		Findings []map[string]any `json:"findings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&ward); err != nil {
		t.Fatal(err)
	}
	if ward.Name != "amdfinancials" || ward.Archetype != "research" {
		t.Fatalf("ward identity: %+v", ward)
	}
	if ward.Layout == nil || ward.Layout.Kind != "directory" || len(ward.Layout.Children) == 0 {
		t.Fatalf("layout tree must carry root + children: %+v", ward.Layout)
	}
	first := ward.Layout.Children[0]
	if first.ID != "canonical" || first.Kind != "file" || first.Match != "{ward}.md" {
		t.Fatalf("tree node ids/kinds/matches must survive: %+v", first)
	}
	if len(ward.Findings) != 0 {
		t.Fatalf("clean ward findings: %+v", ward.Findings)
	}
}

func TestWardsDetailViolationsAnd404(t *testing.T) {
	t.Parallel()
	daemon, _ := newWardsServer(t)
	defer daemon.Close()

	// The violating ward carries its findings.
	res, err := http.Get(daemon.URL + "/api/wards/quant-strategy")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var ward struct {
		LintStatus string `json:"lint_status"`
		Findings   []struct {
			Kind   string  `json:"kind"`
			NodeID *string `json:"node_id"`
			Path   *string `json:"path"`
		} `json:"findings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&ward); err != nil {
		t.Fatal(err)
	}
	if ward.LintStatus != "violations" || len(ward.Findings) != 2 {
		t.Fatalf("violating ward: %+v", ward)
	}
	kinds := map[string]int{}
	for _, f := range ward.Findings {
		kinds[f.Kind]++
		if f.Kind == "missing_required" && (f.NodeID == nil || *f.NodeID != "canonical" || f.Path == nil || *f.Path != "quant-strategy.md") {
			t.Fatalf("missing_required must name node id + expected path: %+v", f)
		}
	}
	if kinds["missing_required"] != 1 || kinds["unexpected_entry"] != 1 {
		t.Fatalf("finding kinds: %+v", kinds)
	}

	// Unknown ward → typed 404.
	res404, err := http.Get(daemon.URL + "/api/wards/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer res404.Body.Close()
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(res404.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res404.StatusCode != http.StatusNotFound || body.Code != "not_found" {
		t.Fatalf("typed 404: %d %+v", res404.StatusCode, body)
	}

	// Path-shaped names never reach the filesystem.
	resEsc, err := http.Get(daemon.URL + "/api/wards/..%2F..")
	if err != nil {
		t.Fatal(err)
	}
	resEsc.Body.Close()
	if resEsc.StatusCode != http.StatusNotFound {
		t.Fatalf("escape attempt must 404, got %d", resEsc.StatusCode)
	}
}

// Unwired vault (nil seam) answers 503, not a panic — both endpoints.
func TestWardsUnwiredIsServiceUnavailable(t *testing.T) {
	t.Parallel()
	srv := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "test")
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	for _, path := range []string{"/api/wards", "/api/wards/someone"} {
		res, err := http.Get(daemon.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable || body.Code != "wards_unavailable" {
			t.Fatalf("%s: want 503 wards_unavailable, got %d %+v", path, res.StatusCode, body)
		}
	}
}

// TestEnsureWardenHandler (final-review P1-3): the warden endpoint —
// 201 on first ensure, 200 after (atomic), 404 for unknown wards, and
// the session carries type + binding.
func TestEnsureWardenHandler(t *testing.T) {
	// A wards daemon WITH a session store (the warden seam).
	root := filepath.Join(t.TempDir(), "wards")
	if err := wards.Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("vault bootstrap: %v", err)
	}
	writeVaultFiles(t, filepath.Join(root, "amdfinancials"), map[string]string{
		"amdfinancials.md": "# AMD financials\nsynthesis\n",
		"AGENTS.md":        "readme\n",
		"log.md":           "log\n",
		"ward-conf.yaml":   researchConfFixture,
	})
	vault, err := wards.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("vault open: %v", err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "wardens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	srv := New(nil, nil, nil, nil, nil, nil, db, nil, nil, nil, vault, "test")
	daemon := httptest.NewServer(srv.Routes())
	t.Cleanup(daemon.Close)

	code, body := mutate(t, "POST", daemon.URL+"/api/wards/amdfinancials/warden", nil)
	if code != http.StatusCreated {
		t.Fatalf("first ensure: %d %v", code, body)
	}
	if body["id"] != "ward-amdfinancials" || body["type"] != "research" {
		t.Fatalf("warden shape: %v", body)
	}
	code, body = mutate(t, "POST", daemon.URL+"/api/wards/amdfinancials/warden", nil)
	if code != http.StatusOK || body["id"] != "ward-amdfinancials" {
		t.Fatalf("second ensure must reuse (200): %d %v", code, body)
	}
	code, _ = mutate(t, "POST", daemon.URL+"/api/wards/no-such-ward/warden", nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown ward: %d", code)
	}
}
