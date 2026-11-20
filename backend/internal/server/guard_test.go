package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMutationGuard(t *testing.T) {
	daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {}, nil)

	n := 0
	post := func(origin, contentType string) int {
		n++
		req, _ := http.NewRequest("POST", daemon.URL+"/api/providers", strings.NewReader(fmt.Sprintf(`{"name":"g%d","base_url":"http://x:1/v1"}`, n)))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	t.Run("same-origin JSON accepted", func(t *testing.T) {
		if code := post("http://127.0.0.1:8787", "application/json"); code != http.StatusCreated {
			t.Fatalf("want 201, got %d", code)
		}
	})

	t.Run("no Origin (curl/CLI) accepted", func(t *testing.T) {
		if code := post("", "application/json"); code != http.StatusCreated {
			t.Fatalf("want 201, got %d", code)
		}
	})

	t.Run("hostile origin forbidden", func(t *testing.T) {
		if code := post("http://evil.example", "application/json"); code != http.StatusForbidden {
			t.Fatalf("want 403, got %d", code)
		}
	})

	t.Run("non-JSON content type rejected (form-post CSRF vector)", func(t *testing.T) {
		if code := post("http://evil.example", "text/plain"); code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d", code)
		}
		// Even same-origin text/plain is refused: mutations are JSON.
		if code := post("", "application/x-www-form-urlencoded"); code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d", code)
		}
	})

	t.Run("GET needs no guard", func(t *testing.T) {
		resp, err := http.Get(daemon.URL + "/api/providers")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET should pass the guard, got %d", resp.StatusCode)
		}
	})

	t.Run("DELETE without content type allowed", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", daemon.URL+"/api/providers/ghost", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnsupportedMediaType {
			t.Fatal("DELETE (no body) must not require a content type")
		}
	})
}

// TestCSRFExfiltrationPathBlocked reproduces the P0 scenario: a hostile
// page POSTs a provider whose env credential reference names a real
// secret and whose base URL points at the attacker, then triggers model
// discovery to exfiltrate the value. The guard must stop the POST.
func TestCSRFExfiltrationPathBlocked(t *testing.T) {
	var dialed bool
	daemon, _, _ := newTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		dialed = true // the "attacker" endpoint
		_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
	}, nil)

	req, _ := http.NewRequest("POST", daemon.URL+"/api/providers",
		strings.NewReader(`{"name":"evil","base_url":"http://127.0.0.1:1/v1","credential":{"kind":"env","env_name":"HOME"}}`))
	req.Header.Set("Content-Type", "text/plain") // the simple-request vector
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("hostile cross-origin mutation must be refused, got %d", resp.StatusCode)
	}
	// And the JSON variant from a hostile origin:
	req2, _ := http.NewRequest("POST", daemon.URL+"/api/providers",
		strings.NewReader(`{"name":"evil","base_url":"http://127.0.0.1:1/v1","credential":{"kind":"env","env_name":"HOME"}}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Origin", "http://evil.example")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("hostile origin must be forbidden, got %d", resp2.StatusCode)
	}
	if dialed {
		t.Fatal("attacker endpoint must never be dialed")
	}
}
