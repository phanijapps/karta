package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// postJSON issues a JSON-content-type POST (the mutation guard requires
// it) with an optional body.
func postJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp.StatusCode, parsed
}

func TestListModelsCatalog(t *testing.T) {
	t.Run("empty catalog on fresh daemon", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := getJSON(t, daemon.URL+"/api/models")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d", code)
		}
		if models := body["models"].([]any); len(models) != 0 {
			t.Fatalf("fresh catalog must be empty: %v", body)
		}
		if _, has := body["refreshed_at"]; has {
			t.Fatalf("no refresh timestamps expected: %v", body)
		}
	})

	t.Run("catalog read never dials providers", func(t *testing.T) {
		var dialed bool
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			dialed = true
		}, nil)
		code, _ := getJSON(t, daemon.URL+"/api/models")
		if code != http.StatusOK || dialed {
			t.Fatalf("catalog read must not dial providers (code %d, dialed %v)", code, dialed)
		}
	})

	t.Run("discovery warms the catalog", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"glm-4.7"},{"id":"qwen:7b"}]}`)
		}, nil)
		if code, _ := getJSON(t, daemon.URL+"/api/providers/ollama/models"); code != http.StatusOK {
			t.Fatal("discovery setup failed")
		}
		code, body := getJSON(t, daemon.URL+"/api/models")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d", code)
		}
		models := body["models"].([]any)
		if len(models) != 2 {
			t.Fatalf("catalog must hold the discovery result: %v", body)
		}
		first := models[0].(map[string]any)
		if first["provider"] != "ollama" || first["id"] != "glm-4.7" {
			t.Fatalf("flat entry shape: %v", first)
		}
		refreshed := body["refreshed_at"].(map[string]any)
		if _, ok := refreshed["ollama"]; !ok {
			t.Fatalf("refresh timestamp missing: %v", refreshed)
		}
	})
}

func TestRefreshProviderModels(t *testing.T) {
	t.Run("refresh updates catalog and returns models", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"glm-4.7"}]}`)
		}, nil)
		code, body := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil)
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d %v", code, body)
		}
		if models := body["models"].([]any); len(models) != 1 || models[0].(map[string]any)["id"] != "glm-4.7" {
			t.Fatalf("refresh response: %v", body)
		}
		if _, catalogBody := getJSON(t, daemon.URL+"/api/models"); len(catalogBody["models"].([]any)) != 1 {
			t.Fatalf("catalog must be updated: %v", catalogBody)
		}
	})

	t.Run("unknown provider is 404", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := postJSON(t, "POST", daemon.URL+"/api/providers/ghost/models/refresh", nil)
		if code != http.StatusNotFound || body["code"] != "not_found" {
			t.Fatalf("want typed 404, got %d %v", code, body)
		}
	})

	t.Run("failed refresh keeps the previous catalog entry", func(t *testing.T) {
		var fail bool
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable) // provider breaks mid-life
				return
			}
			_, _ = fmt.Fprint(w, `{"data":[{"id":"glm-4.7"}]}`)
		}, nil)
		if code, _ := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil); code != http.StatusOK {
			t.Fatal("initial refresh failed")
		}
		fail = true
		code, body := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil)
		if code != http.StatusBadGateway || body["reason"] != "malformed" {
			t.Fatalf("want typed failure, got %d %v", code, body)
		}
		_, catalogBody := getJSON(t, daemon.URL+"/api/models")
		if models := catalogBody["models"].([]any); len(models) != 1 {
			t.Fatalf("previous catalog entry must survive: %v", catalogBody)
		}
	})
}

func TestRefreshFailureMatrixPreservesCatalog(t *testing.T) {
	// Every typed failure must preserve the previous list AND timestamp.
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"unreachable", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{"unauthorized", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `<html>`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fail bool
			daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if fail {
					tc.handler(w, r)
					return
				}
				_, _ = fmt.Fprint(w, `{"data":[{"id":"original"}]}`)
			}, nil)
			if code, _ := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil); code != http.StatusOK {
				t.Fatal("setup refresh failed")
			}
			_, before := getJSON(t, daemon.URL+"/api/models")
			beforeTS := before["refreshed_at"].(map[string]any)["ollama"]

			fail = true
			code, body := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil)
			if code != http.StatusBadGateway {
				t.Fatalf("want typed failure, got %d %v", code, body)
			}
			_, after := getJSON(t, daemon.URL+"/api/models")
			models := after["models"].([]any)
			if len(models) != 1 || models[0].(map[string]any)["id"] != "original" {
				t.Fatalf("previous list must be preserved exactly: %v", models)
			}
			if got := after["refreshed_at"].(map[string]any)["ollama"]; got != beforeTS {
				t.Fatalf("timestamp must be preserved: before %v after %v", beforeTS, got)
			}
		})
	}
}

func TestCatalogReconciledWithProviderLifecycle(t *testing.T) {
	daemon, _, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
	}, nil)
	// Refresh the config-origin provider, then confirm the entry exists.
	if code, _ := postJSON(t, "POST", daemon.URL+"/api/providers/ollama/models/refresh", nil); code != http.StatusOK {
		t.Fatal("refresh failed")
	}
	// Create + refresh a managed provider (pointed at the fake).
	if code, _ := postJSON(t, "POST", daemon.URL+"/api/providers", map[string]any{"name": "temp", "base_url": h.fake.URL + "/v1"}); code != http.StatusCreated {
		t.Fatal("create failed")
	}
	if code, _ := postJSON(t, "POST", daemon.URL+"/api/providers/temp/models/refresh", nil); code != http.StatusOK {
		t.Fatal("temp refresh failed")
	}
	if _, body := getJSON(t, daemon.URL+"/api/models"); len(body["models"].([]any)) != 2 {
		t.Fatalf("both providers catalogued: %v", body)
	}
	// Delete the managed provider: its catalog entries and timestamp drop
	// out at read time without an explicit catalog mutation.
	if code, _ := postJSON(t, "DELETE", daemon.URL+"/api/providers/temp", nil); code != http.StatusNoContent {
		t.Fatal("delete failed")
	}
	_, body := getJSON(t, daemon.URL+"/api/models")
	if models := body["models"].([]any); len(models) != 1 || models[0].(map[string]any)["provider"] != "ollama" {
		t.Fatalf("deleted provider's entries must drop out: %v", body)
	}
	if refreshed := body["refreshed_at"].(map[string]any); len(refreshed) != 1 {
		t.Fatalf("deleted provider's timestamp must drop out: %v", refreshed)
	}
}
