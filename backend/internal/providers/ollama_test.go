package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFakeOllama serves Ollama's native API over httptest: /api/version
// and /api/show with one context_length per family. showHits counts the
// per-model requests.
func newFakeOllama(t *testing.T, windows map[string]int) (*httptest.Server, *int) {
	t.Helper()
	showHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.34.2"}`))
		case "/api/show":
			showHits++
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w32, ok := windows[body.Model]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			family := map[string]string{"glm": "glm5_next", "qwen": "qwen3", "llama": "llama"}[body.Model]
			_, _ = w.Write([]byte(`{"model_info":{"general.architecture":"` + family + `","` + family + `.context_length":` + itoa(w32) + `,"` + family + `.block_count":40}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &showHits
}

func itoa(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Every Ollama model reports <family>.context_length; one suffix match
// covers every family, and the OpenAI-compatible base (…/v1) maps onto
// the native root.
func TestDiscoverWindowsOllama(t *testing.T) {
	srv, showHits := newFakeOllama(t, map[string]int{"glm": 1048576, "qwen": 40960})
	c := NewClient()
	p := Provider{BaseURL: srv.URL + "/v1"}
	models := []Model{{ID: "glm"}, {ID: "qwen"}, {ID: "missing"}}
	out := c.DiscoverWindows(context.Background(), p, "", models)
	if out[0].ContextWindow != 1048576 || out[1].ContextWindow != 40960 {
		t.Fatalf("windows: %+v", out)
	}
	if out[2].ContextWindow != 0 {
		t.Fatalf("an unknown model keeps its window absent: %+v", out[2])
	}
	if *showHits != 3 {
		t.Fatalf("one /api/show per model, got %d", *showHits)
	}
}

// A provider without Ollama's native API costs exactly one request
// (the version probe) and every entry keeps its window absent.
func TestDiscoverWindowsNonOllama(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	models := []Model{{ID: "glm-5.3-flash"}, {ID: "glm-5.3"}}
	out := c.DiscoverWindows(context.Background(), Provider{BaseURL: srv.URL + "/v1"}, "tok", models)
	if hits != 1 {
		t.Fatalf("non-Ollama provider: want 1 probe, got %d", hits)
	}
	for _, m := range out {
		if m.ContextWindow != 0 {
			t.Fatalf("no window for a non-Ollama provider: %+v", m)
		}
	}
}

func TestOllamaRoot(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:11434/v1":      "http://localhost:11434",
		"http://localhost:11434/v1/":     "http://localhost:11434",
		"http://localhost:11434":         "http://localhost:11434",
		"https://api.example.com/v1":     "https://api.example.com",
		"https://api.example.com/av1":    "https://api.example.com/av1",
		"https://api.example.com/v1beta": "https://api.example.com/v1beta",
	} {
		if got := ollamaRoot(in); got != want {
			t.Errorf("ollamaRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscoverWindowsEmpty(t *testing.T) {
	c := NewClient()
	if got := c.DiscoverWindows(context.Background(), Provider{BaseURL: "http://127.0.0.1:1"}, "", nil); got != nil {
		t.Fatalf("empty list: %+v", got)
	}
}
