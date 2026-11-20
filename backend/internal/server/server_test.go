package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/config"
	"zbot/backend/internal/providers"
	zbotstore "zbot/backend/internal/store"
)

// newTestDaemon builds the daemon over a temp home (config.yaml entries +
// managed provider files) and an in-process fake provider endpoint. Unit
// tests never leave the machine.
func newTestDaemon(t *testing.T, providerHandler http.HandlerFunc, cfgProviders []config.ProviderConfig) (*httptest.Server, *providers.Store, *fakeSecretsHarness) {
	t.Helper()
	fake := httptest.NewServer(providerHandler)
	t.Cleanup(fake.Close)

	if cfgProviders == nil {
		cfgProviders = []config.ProviderConfig{
			{Name: "ollama", Type: "openai-compatible", BaseURL: fake.URL + "/v1"},
			{Name: "zai", Type: "openai-compatible", BaseURL: "https://api.z.ai/v1", APIKeyEnv: "ZAI_API_KEY"},
		}
	}
	home := t.TempDir()
	paths := config.NewPaths(home)
	if err := config.Bootstrap(paths); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Providers = cfgProviders
	secrets := newFakeSecrets()
	store, err := providers.NewStore(paths, cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	catalog := providers.NewCatalog()
	daemon := httptest.NewServer(New(store, providers.NewClient(), catalog, newFakeChatService(), "test").Routes())
	t.Cleanup(daemon.Close)
	return daemon, store, &fakeSecretsHarness{fake: fake, secrets: secrets, paths: paths, catalog: catalog}
}

type fakeSecretsHarness struct {
	fake    *httptest.Server
	secrets *fakeSecrets
	paths   config.Paths
	catalog *providers.Catalog
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, body
}

func TestGetHealth(t *testing.T) {
	daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
	code, body := getJSON(t, daemon.URL+"/healthz")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["service"] != "zbotd" || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("unexpected health body: %v", body)
	}
}

func TestListProviders(t *testing.T) {
	t.Run("origins, credential status, and no secrets", func(t *testing.T) {
		t.Setenv("ZAI_API_KEY", "super-secret-value")
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := getJSON(t, daemon.URL+"/api/providers")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		list := body["providers"].([]any)
		if len(list) != 2 {
			t.Fatalf("want 2 providers, got: %v", body)
		}
		first := list[0].(map[string]any)
		if first["origin"] != "config" || first["credential_status"] != "unset" || first["credential"] != "none" {
			t.Fatalf("unexpected config-origin provider: %v", first)
		}
		second := list[1].(map[string]any)
		if second["credential"] != "env" || second["credential_status"] != "ready" || second["credential_configured"] != true {
			t.Fatalf("unexpected env credential status: %v", second)
		}
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "super-secret-value") {
			t.Fatal("credential value leaked in provider list")
		}
	})

	t.Run("empty list when nothing configured", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, []config.ProviderConfig{})
		code, body := getJSON(t, daemon.URL+"/api/providers")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if list := body["providers"].([]any); len(list) != 0 {
			t.Fatalf("want empty list, got: %v", body)
		}
	})
}

func TestListProviderModels(t *testing.T) {
	t.Run("success proxies discovered models", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("provider path = %q, want /v1/models", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"glm-5.3"},{"id":"qwen:7b"}]}`)
		}, nil)
		code, body := getJSON(t, daemon.URL+"/api/providers/ollama/models")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		models := body["models"].([]any)
		if len(models) != 2 || models[0].(map[string]any)["id"] != "glm-5.3" {
			t.Fatalf("unexpected models: %v", body)
		}
	})

	t.Run("unknown provider is typed 404", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
		code, body := getJSON(t, daemon.URL+"/api/providers/nope/models")
		if code != http.StatusNotFound || body["code"] != "not_found" {
			t.Fatalf("want typed 404, got: %d %v", code, body)
		}
	})

	t.Run("provider auth failure is typed 502 unauthorized", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}, nil)
		code, body := getJSON(t, daemon.URL+"/api/providers/ollama/models")
		if code != http.StatusBadGateway || body["reason"] != "unauthorized" {
			t.Fatalf("want typed unauthorized, got: %d %v", code, body)
		}
	})

	t.Run("unreachable provider is typed 502 unreachable", func(t *testing.T) {
		var fakeURL string
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		fakeURL = fake.URL
		fake.Close() // nothing listens: the provider is down
		cfg := []config.ProviderConfig{{Name: "down", Type: "openai-compatible", BaseURL: fakeURL + "/v1"}}
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, cfg)
		code, body := getJSON(t, daemon.URL+"/api/providers/down/models")
		if code != http.StatusBadGateway || body["reason"] != "unreachable" {
			t.Fatalf("want typed unreachable, got: %d %v", code, body)
		}
	})

	t.Run("malformed provider body is typed 502 malformed", func(t *testing.T) {
		daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `<html>not json</html>`)
		}, nil)
		code, body := getJSON(t, daemon.URL+"/api/providers/ollama/models")
		if code != http.StatusBadGateway || body["reason"] != "malformed" {
			t.Fatalf("want typed malformed, got: %d %v", code, body)
		}
	})
}

func TestUnimplementedContractOperations(t *testing.T) {
	// The contract is ahead of implementation: goal/session operations
	// stay on the generated 501 stubs (bare status, no body) until their
	// owning change lands.
	daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
	for _, path := range []string{"/api/sessions", "/api/sessions/x"} {
		resp, err := http.Get(daemon.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("GET %s = %d, want 501", path, resp.StatusCode)
		}
	}
}

// newFakeChatService gives handler tests an empty transcript service.
func newFakeChatService() ChatService { return &stubChatService{} }

type stubChatService struct{}

func (s *stubChatService) Send(ctx context.Context, text, providerName, model string) chat.SendOutcome {
	return chat.SendOutcome{Err: fmt.Errorf("stub")}
}
func (s *stubChatService) Sync(afterSeq int64, limit int) ([]zbotstore.Message, int64, bool, error) {
	return nil, afterSeq, false, nil
}
