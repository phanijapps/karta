package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
)

// fakeProber returns canned per-transport results without spawning
// anything (zero processes, zero network in handler tests).
type fakeProber struct {
	result mcpclient.ProbeResult
	spec   registry.MCPSpec
}

func (p *fakeProber) Test(ctx context.Context, spec registry.MCPSpec) mcpclient.ProbeResult {
	p.spec = spec
	return p.result
}

// newTestServer builds a Server over a temp-home registry (mcps/skills
// dirs exist) and a fake prober, mirroring the daemon wiring.
func newTestServer(store *providers.Store, catalog *providers.Catalog) *Server {
	return newTestServerWithProber(store, catalog, &fakeProber{})
}

func newTestServerWithProber(store *providers.Store, catalog *providers.Catalog, prober mcpclient.Prober) *Server {
	root := tTempHome()
	dirs := registry.Dirs{
		Prompts:    filepath.Join(root, "config", "prompts"),
		Agents:     filepath.Join(root, "config", "agents"),
		Skills:     filepath.Join(root, "config", "skills"),
		Hooks:      filepath.Join(root, "config", "hooks"),
		MCPs:       filepath.Join(root, "config", "mcps"),
		ConfigFile: filepath.Join(root, "config.yaml"),
	}
	for _, dir := range []string{dirs.Prompts, dirs.Agents, dirs.Skills, dirs.Hooks, dirs.MCPs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
	}
	reg := registry.New()
	reg.LoadAll(dirs)
	lastTestDirs = dirs
	return New(store, providers.NewClient(), catalog, newFakeChatService(), nil, nil, nil, reg, prober, "test")
}

// lastTestDirs remembers the dirs of the most recently built test server
// (registryDirsOf only works when a snapshot has entries).
var lastTestDirs registry.Dirs

// tTempHome builds an isolated throwaway home root for one test server.
func tTempHome() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("zbot-server-test-%d", time.Now().UnixNano()))
}

func mustDo(t *testing.T, daemon *httptest.Server, method, path string, body any) (int, string) {
	t.Helper()
	var payload *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(raw)
	} else {
		payload = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, daemon.URL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := daemon.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, buf.String()
}

func TestMcpCrudHandlers(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()

	// Create (stdio, dual with http) → 201, listable at once.
	code, body := mustDo(t, daemon, "POST", "/api/mcps", map[string]any{
		"name":  "github",
		"stdio": map[string]any{"command": "gh-mcp", "args": []string{"serve"}, "env": map[string]string{"TOKEN": "t"}},
		"http":  map[string]any{"url": "http://127.0.0.1:9999/mcp"},
	})
	if code != http.StatusCreated || !strings.Contains(body, "\"transports\":[{\"args\":[\"serve\"]") {
		t.Fatalf("create: %d %s", code, body)
	}
	code, body = mustDo(t, daemon, "GET", "/api/mcps", nil)
	if code != http.StatusOK || !strings.Contains(body, "github") || !strings.Contains(body, "\"stdio\"") || !strings.Contains(body, "\"http\"") {
		t.Fatalf("list: %d %s", code, body)
	}

	// Duplicate name → 409; invalid name → 400; no transport → 400.
	code, body = mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "github", "stdio": map[string]any{"command": "x"}})
	if code != http.StatusConflict || !strings.Contains(body, "duplicate_name") {
		t.Fatalf("duplicate: %d %s", code, body)
	}
	code, _ = mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "Bad_Name", "stdio": map[string]any{"command": "x"}})
	if code != http.StatusBadRequest {
		t.Fatalf("invalid name: %d", code)
	}
	code, body = mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "empty"})
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid_manifest") {
		t.Fatalf("no transport: %d %s", code, body)
	}

	// Edit full-replace clearing both transports → 400.
	code, _ = mustDo(t, daemon, "PATCH", "/api/mcps/github", map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("clearing both transports: %d", code)
	}

	// Rename onto occupied path → 409 duplicate_name (snapshot-blind
	// occupant: an invalid file).
	regDirs := srv.reg.Snapshot()
	_ = regDirs
	mcpDir := filepath.Dir(srv.reg.Snapshot().MCPs["github"].Path)
	if err := os.WriteFile(filepath.Join(mcpDir, "ghost.yaml"), []byte("args:\n  - x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.reg.LoadAll(registryDirsOf(srv))
	code, body = mustDo(t, daemon, "PATCH", "/api/mcps/github", map[string]any{"name": "ghost", "stdio": map[string]any{"command": "x"}})
	if code != http.StatusConflict || !strings.Contains(body, "duplicate_name") {
		t.Fatalf("occupied rename: %d %s", code, body)
	}

	// Edit in place → 200; unknown → 404; delete → 204 then 404.
	code, body = mustDo(t, daemon, "PATCH", "/api/mcps/github", map[string]any{"stdio": map[string]any{"command": "renamed-cmd"}})
	if code != http.StatusOK || !strings.Contains(body, "renamed-cmd") {
		t.Fatalf("edit: %d %s", code, body)
	}
	code, _ = mustDo(t, daemon, "PATCH", "/api/mcps/missing", map[string]any{"stdio": map[string]any{"command": "x"}})
	if code != http.StatusNotFound {
		t.Fatalf("unknown edit: %d", code)
	}
	code, _ = mustDo(t, daemon, "DELETE", "/api/mcps/github", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	code, _ = mustDo(t, daemon, "DELETE", "/api/mcps/github", nil)
	if code != http.StatusNotFound {
		t.Fatalf("double delete: %d", code)
	}
}

func registryDirsOf(srv *Server) registry.Dirs {
	// Reconstruct the dirs the test server was built with from any
	// loaded MCP's path (mcps dir).
	for _, spec := range srv.reg.Snapshot().MCPs {
		dir := filepath.Dir(spec.Path)
		return registry.Dirs{MCPs: dir, Skills: filepath.Join(filepath.Dir(dir), "skills")}
	}
	for _, spec := range srv.reg.Snapshot().Skills {
		dir := filepath.Dir(spec.Path)
		return registry.Dirs{Skills: dir, MCPs: filepath.Join(filepath.Dir(dir), "mcps")}
	}
	return registry.Dirs{}
}

func TestMcpListCarriesLoadErrors(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "ok", "stdio": map[string]any{"command": "x"}})
	// Hand-edit a broken manifest straight to disk, reload through a
	// mutation-free path (LoadAll on the same dirs via a second create).
	dirs := registryDirsOf(srv)
	if err := os.WriteFile(filepath.Join(dirs.MCPs, "broken.yaml"), []byte("args:\n  - nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "ok2", "stdio": map[string]any{"command": "x"}})
	code, body := mustDo(t, daemon, "GET", "/api/mcps", nil)
	if code != http.StatusOK || !strings.Contains(body, "load_errors") || !strings.Contains(body, "broken.yaml") {
		t.Fatalf("load errors missing: %d %s", code, body)
	}
}

func TestMcpTestHandler(t *testing.T) {
	prober := &fakeProber{result: mcpclient.ProbeResult{Results: []mcpclient.TransportResult{
		{Transport: "stdio", Status: "ok", Tools: []mcpclient.Tool{{Name: "echo", Description: "Echoes"}}},
		{Transport: "http", Status: "failed", Failure: &mcpclient.Failure{Reason: "connect_failed", Message: "refused"}},
	}}}
	srv := newTestServerWithProber(nil, providers.NewCatalog(), prober)
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "dual", "stdio": map[string]any{"command": "x"}, "http": map[string]any{"url": "http://127.0.0.1:1/mcp"}})

	code, body := mustDo(t, daemon, "POST", "/api/mcps/dual/test", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("test: %d %s", code, body)
	}
	var parsed struct {
		Results []struct {
			Transport string `json:"transport"`
			Status    string `json:"status"`
			Tools     []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Failure *struct {
				Reason string `json:"reason"`
			} `json:"failure"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("want 2 per-transport results: %s", body)
	}
	if parsed.Results[0].Status != "ok" || len(parsed.Results[0].Tools) != 1 || parsed.Results[0].Tools[0].Name != "echo" {
		t.Fatalf("stdio result wrong: %s", body)
	}
	if parsed.Results[1].Status != "failed" || parsed.Results[1].Failure == nil || parsed.Results[1].Failure.Reason != "connect_failed" {
		t.Fatalf("http result wrong: %s", body)
	}

	// Probe failures are 200 bodies; unknown server is a 404.
	code, _ = mustDo(t, daemon, "POST", "/api/mcps/missing/test", map[string]any{})
	if code != http.StatusNotFound {
		t.Fatalf("unknown test: %d", code)
	}
}

func TestSkillCrudHandlers(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()

	// Create → 201; detail round-trips the body.
	code, body := mustDo(t, daemon, "POST", "/api/skills", map[string]any{
		"name": "Project Planning", "description": "Break work into steps.", "body": "# Plan\n",
	})
	if code != http.StatusCreated || !strings.Contains(body, "project-planning") {
		t.Fatalf("create: %d %s", code, body)
	}
	code, body = mustDo(t, daemon, "GET", "/api/skills/Project%20Planning", nil)
	if code != http.StatusOK || !strings.Contains(body, "# Plan") {
		t.Fatalf("detail: %d %s", code, body)
	}

	// Empty description → 400; duplicate name → 409.
	code, _ = mustDo(t, daemon, "POST", "/api/skills", map[string]any{"name": "x", "description": " "})
	if code != http.StatusBadRequest {
		t.Fatalf("empty description: %d", code)
	}
	code, body = mustDo(t, daemon, "POST", "/api/skills", map[string]any{"name": "Project Planning", "description": "other"})
	if code != http.StatusConflict || !strings.Contains(body, "duplicate_name") {
		t.Fatalf("duplicate: %d %s", code, body)
	}

	// Edit re-sending the current body (full replace) → 200.
	code, body = mustDo(t, daemon, "PATCH", "/api/skills/Project%20Planning", map[string]any{"description": "v2", "body": "# Plan"})
	if code != http.StatusOK || !strings.Contains(body, "# Plan") || !strings.Contains(body, "v2") {
		t.Fatalf("edit: %d %s", code, body)
	}

	// Rename → directory moves; old name 404.
	code, body = mustDo(t, daemon, "PATCH", "/api/skills/Project%20Planning", map[string]any{"name": "New Name", "description": "v3"})
	if code != http.StatusOK || !strings.Contains(body, "new-name") {
		t.Fatalf("rename: %d %s", code, body)
	}
	code, _ = mustDo(t, daemon, "GET", "/api/skills/Project%20Planning", nil)
	if code != http.StatusNotFound {
		t.Fatalf("old name after rename: %d", code)
	}

	// Delete removes the directory (bundle included).
	skillDir := srv.reg.Snapshot().Skills["New Name"].Path
	if err := os.WriteFile(filepath.Join(skillDir, "bundle.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ = mustDo(t, daemon, "DELETE", "/api/skills/New%20Name", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatal("skill dir still present")
	}
	code, _ = mustDo(t, daemon, "DELETE", "/api/skills/New%20Name", nil)
	if code != http.StatusNotFound {
		t.Fatalf("double delete: %d", code)
	}
}

func TestGuardHostValidation(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()

	// Legitimate loopback GET (env-bearing list) passes with its own Host.
	code, _ := mustDo(t, daemon, "GET", "/api/mcps", nil)
	if code != http.StatusOK {
		t.Fatalf("loopback GET rejected: %d", code)
	}

	// A DNS-rebound style foreign Host is rejected on reads AND writes.
	req, _ := http.NewRequest("GET", daemon.URL+"/api/mcps", nil)
	req.Host = "rebind.attacker.example"
	res, err := daemon.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-host GET: %d", res.StatusCode)
	}

	req, _ = http.NewRequest("POST", daemon.URL+"/api/mcps", bytes.NewReader([]byte(`{"name":"evil"}`)))
	req.Host = "rebind.attacker.example"
	req.Header.Set("Content-Type", "application/json")
	res, err = daemon.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-host POST: %d", res.StatusCode)
	}

	// A rebound-style same-host Origin on a mutation is rejected because
	// the Host itself is foreign.
	req, _ = http.NewRequest("POST", daemon.URL+"/api/mcps", bytes.NewReader([]byte(`{"name":"evil"}`)))
	req.Host = "rebind.attacker.example"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://rebind.attacker.example")
	res, err = daemon.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rebound same-host origin: %d", res.StatusCode)
	}

	// localhost Host (loopback alias) passes.
	req, _ = http.NewRequest("GET", daemon.URL+"/api/skills", nil)
	req.Host = "localhost"
	res, err = daemon.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("localhost GET: %d", res.StatusCode)
	}
}
