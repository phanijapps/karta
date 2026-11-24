package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"zbot/backend/internal/providers"
)

// mutate runs one JSON request against the daemon and returns status + body.
func mutate(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
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

func TestCreateProvider(t *testing.T) {
	t.Run("create with keyring credential", func(t *testing.T) {
		daemon, _, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"glm-5.3"}]}`)
		}, nil)
		code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{
			"name":       "zai-managed",
			"base_url":   h.fake.URL + "/v1",
			"credential": map[string]any{"kind": "keyring", "key": "one-shot-key"},
		})
		if code != http.StatusCreated {
			t.Fatalf("status = %d, body %v", code, body)
		}
		if body["origin"] != "file" || body["credential"] != "keyring" || body["credential_status"] != "ready" {
			t.Fatalf("unexpected view: %v", body)
		}
		raw, _ := json.Marshal(body)
		if bytes.Contains(raw, []byte("one-shot-key")) {
			t.Fatal("key value leaked in response")
		}
		// Secret stored under the provider's name; file on disk.
		if v, err := h.secrets.Get(t.Context(), "zai-managed"); err != nil || v != "one-shot-key" {
			t.Fatalf("keyring round-trip: %q %v", v, err)
		}
		if _, err := os.Stat(filepath.Join(h.paths.ProvidersDir, "zai-managed.yaml")); err != nil {
			t.Fatalf("managed file missing: %v", err)
		}
		// Immediately usable: model discovery through the daemon.
		code, _ = getJSON(t, daemon.URL+"/api/providers/zai-managed/models")
		if code != http.StatusOK {
			t.Fatalf("created provider must be immediately testable, got %d", code)
		}
	})

	t.Run("duplicate name conflicts", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{"name": "ollama", "base_url": "http://localhost:2/v1"})
		if code != http.StatusConflict || body["code"] != "duplicate_name" {
			t.Fatalf("want typed duplicate conflict, got: %d %v", code, body)
		}
	})

	t.Run("unportable name is a 400", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		for _, name := range []string{"Bad Name", "con", stringsTooLong()} {
			code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{"name": name, "base_url": "http://x:1/v1"})
			if code != http.StatusBadRequest || body["code"] != "invalid_provider" {
				t.Fatalf("name %q: want 400 invalid_provider, got: %d %v", name, code, body)
			}
		}
	})

	t.Run("keyring unavailable is a 503 and writes nothing", func(t *testing.T) {
		daemon, store, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		h.secrets.failAll = true
		code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{
			"name": "nokey", "base_url": "http://x:1/v1",
			"credential": map[string]any{"kind": "keyring", "key": "k"},
		})
		if code != http.StatusServiceUnavailable || body["code"] != "keyring_unavailable" {
			t.Fatalf("want typed keyring 503, got: %d %v", code, body)
		}
		if _, err := store.Lookup("nokey"); err == nil {
			t.Fatal("nothing may be published on keyring failure")
		}
		if _, err := os.Stat(filepath.Join(h.paths.ProvidersDir, "nokey.yaml")); !os.IsNotExist(err) {
			t.Fatal("no file may be written on keyring failure")
		}
	})

	t.Run("bad credential combos are 400s", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		for _, cred := range []map[string]any{
			{"kind": "env"},
			{"kind": "keyring"},
			{"kind": "none", "env_name": "X"},
		} {
			code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{"name": "ok-name", "base_url": "http://x:1/v1", "credential": cred})
			if code != http.StatusBadRequest {
				t.Fatalf("credential %v: want 400, got %d %v", cred, code, body)
			}
		}
	})
}

func TestUpdateProvider(t *testing.T) {
	newDaemonWithManaged := func(t *testing.T) (string, *providers.Store, *fakeSecretsHarness) {
		daemon, _, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
		}, nil)
		code, body := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{
			"name": "zai-m", "base_url": h.fake.URL + "/v1",
			"credential": map[string]any{"kind": "keyring", "key": "k1"},
		})
		if code != http.StatusCreated {
			t.Fatalf("setup create: %d %v", code, body)
		}
		return daemon.URL, nil, h
	}

	t.Run("absent credential retains", func(t *testing.T) {
		url, _, h := newDaemonWithManaged(t)
		code, body := mutate(t, "PATCH", url+"/api/providers/zai-m", map[string]any{"base_url": "https://api.z.ai/v2"})
		if code != http.StatusOK || body["credential"] != "keyring" || body["credential_status"] != "ready" {
			t.Fatalf("credential must be retained: %d %v", code, body)
		}
		if v, _ := h.secrets.Get(t.Context(), "zai-m"); v != "k1" {
			t.Fatalf("secret must be untouched, got %q", v)
		}
	})

	t.Run("rename re-keys and old name 404s", func(t *testing.T) {
		url, _, h := newDaemonWithManaged(t)
		code, body := mutate(t, "PATCH", url+"/api/providers/zai-m", map[string]any{"name": "zai-prod"})
		if code != http.StatusOK || body["name"] != "zai-prod" {
			t.Fatalf("rename: %d %v", code, body)
		}
		if v, err := h.secrets.Get(t.Context(), "zai-prod"); err != nil || v != "k1" {
			t.Fatalf("secret must move: %q %v", v, err)
		}
		code, _ = getJSON(t, url+"/api/providers/zai-m/models")
		if code != http.StatusNotFound {
			t.Fatalf("old name must stop resolving, got %d", code)
		}
	})

	t.Run("credential clear removes secret", func(t *testing.T) {
		url, _, h := newDaemonWithManaged(t)
		code, body := mutate(t, "PATCH", url+"/api/providers/zai-m", map[string]any{"credential": map[string]any{"kind": "none"}})
		if code != http.StatusOK || body["credential"] != "none" || body["credential_status"] != "unset" {
			t.Fatalf("clear: %d %v", code, body)
		}
		if _, err := h.secrets.Get(t.Context(), "zai-m"); err == nil {
			t.Fatal("secret must be deleted on clear")
		}
	})

	t.Run("config-origin provider is origin-protected", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := mutate(t, "PATCH", daemon.URL+"/api/providers/ollama", map[string]any{"base_url": "http://x:1/v1"})
		if code != http.StatusConflict || body["code"] != "origin_protected" {
			t.Fatalf("want origin protection, got: %d %v", code, body)
		}
		code, _ = mutate(t, "DELETE", daemon.URL+"/api/providers/ollama", nil)
		if code != http.StatusConflict {
			t.Fatalf("delete of config-origin must conflict, got %d", code)
		}
	})

	t.Run("rename onto existing name conflicts", func(t *testing.T) {
		url, _, _ := newDaemonWithManaged(t)
		code, body := mutate(t, "PATCH", url+"/api/providers/zai-m", map[string]any{"name": "ollama"})
		if code != http.StatusConflict || body["code"] != "duplicate_name" {
			t.Fatalf("want duplicate conflict, got: %d %v", code, body)
		}
	})

	t.Run("unknown provider is 404", func(t *testing.T) {
		url, _, _ := newDaemonWithManaged(t)
		code, _ := mutate(t, "PATCH", url+"/api/providers/ghost", map[string]any{"base_url": "http://x:1/v1"})
		if code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", code)
		}
	})
}

func TestDeleteProvider(t *testing.T) {
	t.Run("delete removes file and secret", func(t *testing.T) {
		daemon, _, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, _ := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{
			"name": "temp", "base_url": "http://x:1/v1",
			"credential": map[string]any{"kind": "keyring", "key": "k"},
		})
		if code != http.StatusCreated {
			t.Fatal("setup create failed")
		}
		code, _ = mutate(t, "DELETE", daemon.URL+"/api/providers/temp", nil)
		if code != http.StatusNoContent {
			t.Fatalf("want 204, got %d", code)
		}
		if _, err := os.Stat(filepath.Join(h.paths.ProvidersDir, "temp.yaml")); !os.IsNotExist(err) {
			t.Fatal("file must be removed")
		}
		if _, err := h.secrets.Get(t.Context(), "temp"); err == nil {
			t.Fatal("secret must be removed")
		}
		code, _ = getJSON(t, daemon.URL+"/api/providers/temp/models")
		if code != http.StatusNotFound {
			t.Fatal("deleted provider must not resolve")
		}
	})

	t.Run("unknown provider is 404", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, _ := mutate(t, "DELETE", daemon.URL+"/api/providers/ghost", nil)
		if code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", code)
		}
	})
}

func TestKeyringUnavailableDiscovery(t *testing.T) {
	// A keyring-credential provider whose keyring goes down mid-flight:
	// discovery must fail typed, never silently unauthenticated.
	daemon, _, h := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("provider must not be dialed when the credential cannot resolve")
	}, nil)
	code, _ := mutate(t, "POST", daemon.URL+"/api/providers", map[string]any{
		"name": "zai-down", "base_url": "http://127.0.0.1:1/v1",
		"credential": map[string]any{"kind": "keyring", "key": "k"},
	})
	if code != http.StatusCreated {
		t.Fatal("setup create failed")
	}
	h.secrets.failAll = true
	code, body := getJSON(t, daemon.URL+"/api/providers/zai-down/models")
	if code != http.StatusBadGateway || body["reason"] != "credential_unavailable" {
		t.Fatalf("want typed credential_unavailable, got: %d %v", code, body)
	}
}

func stringsTooLong() string {
	b := make([]byte, 65)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// TestSessionCRUD (add-chat-modes 1.2/2.2): POST creates a research
// shell; GET lists sessions with type + ward (the sidebar's grouping
// data) ordered by recent activity; GET one 404s unknowns; sends in
// unknown sessions fail typed 404.
func TestSessionCRUD(t *testing.T) {
	url, db := newChatDaemon(t, nil, nil)

	// Create: a research shell.
	code, body := mutate(t, "POST", url+"/api/sessions", map[string]any{})
	if code != http.StatusCreated {
		t.Fatalf("create status %d: %v", code, body)
	}
	if body["type"] != "research" || body["id"] == "" {
		t.Fatalf("created session: %v", body)
	}
	createdID := body["id"].(string)
	if _, err := db.Session(createdID); err != nil {
		t.Fatalf("persisted: %v", err)
	}

	// Create with a title.
	code, body = mutate(t, "POST", url+"/api/sessions", map[string]any{"title": "Cloud pricing"})
	if code != http.StatusCreated {
		t.Fatalf("titled create status %d: %v", code, body)
	}
	if body["title"] != "Cloud pricing" {
		t.Fatalf("title: %v", body)
	}

	// The default quick chat exists (created above by newChatDaemon's
	// store? no — ensure it) and lists with the shells.
	if _, err := db.EnsureSession("default", "chat"); err != nil {
		t.Fatal(err)
	}
	code, body = mutate(t, "GET", url+"/api/sessions?limit=50", nil)
	if code != http.StatusOK {
		t.Fatalf("list status %d: %v", code, body)
	}
	sessions := body["sessions"].([]any)
	if len(sessions) != 3 {
		t.Fatalf("want 3 sessions, got %d: %v", len(sessions), body)
	}
	first := sessions[0].(map[string]any)
	if first["id"] != "default" || first["type"] != "chat" {
		t.Fatalf("default first (recency): %v", first)
	}

	// Ward binding lands in the list (the sidebar's Ward Bots group).
	if err := db.SetSessionWard(createdID, "stock-analysis"); err != nil {
		t.Fatal(err)
	}
	_, body = mutate(t, "GET", url+"/api/sessions?limit=50", nil)
	sessions = body["sessions"].([]any)
	found := false
	for _, s := range sessions {
		m := s.(map[string]any)
		if m["id"] == createdID {
			found = true
			if m["ward"] != "stock-analysis" {
				t.Fatalf("ward binding missing: %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("session %s missing from list", createdID)
	}

	// GET one: the row; unknown ids 404.
	code, _ = mutate(t, "GET", url+"/api/sessions/"+createdID, nil)
	if code != http.StatusOK {
		t.Fatalf("get status %d", code)
	}
	code, _ = mutate(t, "GET", url+"/api/sessions/missing", nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown get status %d", code)
	}

	// Sends in unknown sessions fail typed 404 (nothing appended).
	code, _ = mutate(t, "POST", url+"/api/chat/messages", map[string]any{
		"text": "hi", "model": "p/m", "session_id": "missing",
	})
	if code != http.StatusNotFound {
		t.Fatalf("unknown send status %d", code)
	}

	// Clear is session-scoped and 404s unknowns.
	code, _ = mutate(t, "DELETE", url+"/api/chat/messages?session_id=missing", nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown clear status %d", code)
	}
}
