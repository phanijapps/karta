package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/config"
)

func testSet() *Set {
	return New([]config.ProviderConfig{
		{Name: "ollama", Type: "openai-compatible", BaseURL: "http://localhost:11434/v1"},
		{Name: "zai", Type: "openai-compatible", BaseURL: "https://api.z.ai/v1", APIKeyEnv: "ZAI_API_KEY"},
	})
}

func TestSetLookup(t *testing.T) {
	t.Run("configured provider resolves", func(t *testing.T) {
		p, err := testSet().Lookup("ollama")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if p.BaseURL != "http://localhost:11434/v1" || p.APIKeyEnv != "" {
			t.Fatalf("unexpected provider: %+v", p)
		}
	})

	t.Run("unknown name is ErrUnknownProvider", func(t *testing.T) {
		_, err := testSet().Lookup("nope")
		if !errors.Is(err, ErrUnknownProvider) {
			t.Fatalf("want ErrUnknownProvider, got: %v", err)
		}
	})

	t.Run("list preserves config order", func(t *testing.T) {
		var names []string
		for _, p := range testSet().List() {
			names = append(names, p.Name)
		}
		if strings.Join(names, ",") != "ollama,zai" {
			t.Fatalf("want config order, got %v", names)
		}
	})
}

// newFakeProvider starts an in-process HTTP server standing in for an
// OpenAI-compatible provider. Unit tests never leave the machine.
func newFakeProvider(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, &Client{HTTP: srv.Client()}
}

func TestListModels(t *testing.T) {
	t.Run("success returns model ids", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("path = %q, want /v1/models", r.URL.Path)
			}
			if _, ok := r.Header["Authorization"]; ok {
				t.Error("no credential configured — Authorization must be absent")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama3:8b","object":"model","created":1},{"id":"qwen:7b"}]}`))
		})
		models, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL + "/v1"}, "")
		if err != nil {
			t.Fatalf("ListModels: %v", err)
		}
		if len(models) != 2 || models[0].ID != "llama3:8b" || models[1].ID != "qwen:7b" {
			t.Fatalf("unexpected models: %+v", models)
		}
	})

	t.Run("trailing slash in base url tolerated", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("path = %q, want /v1/models", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
		})
		if _, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL + "/v1/"}, ""); err != nil {
			t.Fatalf("ListModels: %v", err)
		}
	})

	t.Run("empty model list is success", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
		})
		models, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		if err != nil {
			t.Fatalf("empty list must succeed, got: %v", err)
		}
		if len(models) != 0 {
			t.Fatalf("want no models, got %+v", models)
		}
	})

	t.Run("resolved token sent as bearer", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer secret-value" {
				t.Errorf("Authorization = %q, want bearer token", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"glm"}]}`))
		})
		if _, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "secret-value"); err != nil {
			t.Fatalf("ListModels: %v", err)
		}
	})

	t.Run("empty token sends no header", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if _, ok := r.Header["Authorization"]; ok {
				t.Error("empty token — Authorization must be absent")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
		})
		if _, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, ""); err != nil {
			t.Fatalf("ListModels: %v", err)
		}
	})

	t.Run("provider 401 is typed unauthorized", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
		_, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonUnauthorized {
			t.Fatalf("want unauthorized failure, got: %v", err)
		}
	})

	t.Run("provider 500 is typed malformed", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		_, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonMalformed {
			t.Fatalf("want malformed failure, got: %v", err)
		}
	})

	t.Run("non-json body is typed malformed", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<html>maintenance</html>`))
		})
		_, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonMalformed {
			t.Fatalf("want malformed failure, got: %v", err)
		}
	})

	t.Run("entry without id is typed malformed", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"object":"model"}]}`))
		})
		_, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonMalformed {
			t.Fatalf("want malformed failure, got: %v", err)
		}
	})

	t.Run("unreachable provider is typed unreachable", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {})
		url := srv.URL
		srv.Close() // nothing listens anymore
		_, err := client.ListModels(context.Background(), Provider{BaseURL: url}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonUnreachable {
			t.Fatalf("want unreachable failure, got: %v", err)
		}
	})

	t.Run("timeout is typed unreachable", func(t *testing.T) {
		srv, client := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
		})
		client.HTTP = &http.Client{Timeout: 30 * time.Millisecond}
		_, err := client.ListModels(context.Background(), Provider{BaseURL: srv.URL}, "")
		var failure *Failure
		if !errors.As(err, &failure) || failure.Reason != ReasonUnreachable {
			t.Fatalf("want unreachable failure, got: %v", err)
		}
	})
}
