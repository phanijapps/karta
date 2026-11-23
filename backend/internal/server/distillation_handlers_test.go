package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"zbot/backend/internal/config"
	"zbot/backend/internal/providers"
)

func TestDistillationHandlers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZBOT_HOME", home)
	_ = os.MkdirAll(filepath.Join(home, "config"), 0o755)

	paths := config.NewPaths(home)
	if err := config.Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Providers = []config.ProviderConfig{{Name: "ollama", Type: "openai-compatible", BaseURL: "http://localhost:11434"}}
	store, err := providers.NewStore(paths, cfg, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}

	t.Run("GET empty initially", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.GetDistillation(w, httptest.NewRequest(http.MethodGet, "/api/distillation", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET: %d", w.Code)
		}
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		if m["prompt"] == "" {
			t.Fatal("GET must seed the effective default prompt")
		}
	})

	t.Run("PUT valid round-trips", func(t *testing.T) {
		body := map[string]any{"provider": "ollama", "model": "llama3", "prompt": "PROMPT"}
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.PutDistillation(w, httptest.NewRequest(http.MethodPut, "/api/distillation", bytes.NewReader(raw)))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
		}
		w2 := httptest.NewRecorder()
		s.GetDistillation(w2, httptest.NewRequest(http.MethodGet, "/api/distillation", nil))
		var got map[string]any
		_ = json.Unmarshal(w2.Body.Bytes(), &got)
		if got["provider"] != "ollama" || got["model"] != "llama3" || got["prompt"] != "PROMPT" {
			t.Fatalf("round-trip: %v", got)
		}
	})

	t.Run("PUT unknown provider rejected", func(t *testing.T) {
		body := map[string]any{"provider": "nope", "model": "x"}
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.PutDistillation(w, httptest.NewRequest(http.MethodPut, "/api/distillation", bytes.NewReader(raw)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("unknown provider: %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("nope")) {
			t.Fatalf("error must name the provider: %s", w.Body.String())
		}
	})

	t.Run("PUT empty model rejected", func(t *testing.T) {
		body := map[string]any{"provider": "ollama", "model": ""}
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.PutDistillation(w, httptest.NewRequest(http.MethodPut, "/api/distillation", bytes.NewReader(raw)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("empty model: %d", w.Code)
		}
	})
}
