package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/mcpimport"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	apigen "zbot/backend/internal/server/apigen"
	"zbot/backend/internal/skillhub"
)

// --- handler (design D1–D4 + replace mode) ----------------------------------

func postImport(t *testing.T, daemonURL string, body string) (int, apigen.McpImportResult, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", daemonURL+"/api/mcps/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var parsed apigen.McpImportResult
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(buf.Bytes(), &parsed)
	return res.StatusCode, parsed, buf.String()
}

func importBody(document, name, replaceName string) string {
	b := map[string]any{"document": document}
	if name != "" {
		b["name"] = name
	}
	if replaceName != "" {
		b["replaceName"] = replaceName
	}
	raw, _ := json.Marshal(b)
	return string(raw)
}

func TestImportHandlerMixedPaste(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	// Pre-create "dup" so the paste's duplicate row skips.
	if _, err := srv.reg.CreateMCP("dup", registry.MCPSpec{Command: "existing"}); err != nil {
		t.Fatal(err)
	}
	daemon := startDaemon(t, srv)
	doc := `{"mcpServers": {
		"fresh": {"command": "npx", "args": ["-y", "fs-server"]},
		"dup": {"command": "other"},
		"broken": {"args": ["nope"]}
	}}`
	code, result, body := postImport(t, daemon.URL, importBody(doc, "", ""))
	if code != http.StatusOK {
		t.Fatalf("import: %d %s", code, body)
	}
	if len(result.Results) != 3 {
		t.Fatalf("want 3 outcomes: %s", body)
	}
	// Paste order, per-entry outcomes.
	check := func(i int, status, reason string) {
		t.Helper()
		r := result.Results[i]
		if string(r.Status) != status || (r.Reason != nil && string(*r.Reason) != reason) {
			t.Fatalf("outcome %d: want %s/%s, got %s/%v", i, status, reason, r.Status, r.Reason)
		}
	}
	check(0, mcpimport.StatusCreated, "")
	check(1, mcpimport.StatusSkipped, mcpimport.ReasonDuplicate)
	check(2, mcpimport.StatusFailed, mcpimport.ReasonField)
	if result.Results[1].SourceKey != "dup" || result.Results[2].SourceKey != "broken" {
		t.Fatalf("source keys wrong: %s", body)
	}
	if result.Results[0].Name == nil || *result.Results[0].Name != "fresh" {
		t.Fatalf("created name missing: %s", body)
	}
	if _, ok := srv.reg.Snapshot().MCPs["fresh"]; !ok {
		t.Fatal("fresh not created")
	}
	if srv.reg.Snapshot().MCPs["dup"].Command != "existing" {
		t.Fatal("duplicate was overwritten")
	}
}

func TestImportHandlerDocumentRejections(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := startDaemon(t, srv)
	for name, body := range map[string]string{
		"trailing":    importBody(`{"mcpServers": {"a": {"command": "x"}}} x`, "", ""),
		"dup keys":    importBody(`{"mcpServers": {"a": {"command": "x"}, "a": {"command": "y"}}}`, "", ""),
		"empty":       importBody("", "", ""),
		"no name":     importBody(`{"command": "x"}`, "", ""),
		"bad wrapper": importBody(`{"mcpServers": []}`, "", ""),
	} {
		if code, _, body := postImport(t, daemon.URL, body); code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d (%s)", name, code, body)
		}
	}
	if len(srv.reg.Snapshot().MCPs) != 0 {
		t.Fatal("rejected documents must create nothing")
	}
}

func TestImportHandlerDuplicateOutranksCapacity(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	dirs := lastTestDirs
	for i := 0; i < 128; i++ {
		stem := string(rune('a'+i%26)) + string(rune('a'+i/26))
		if err := os.WriteFile(filepath.Join(dirs.MCPs, stem+".yaml"), []byte("command: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, errs := srv.reg.LoadAll(dirs); len(errs) > 0 {
		t.Fatal(errs)
	}
	daemon := startDaemon(t, srv)
	// Full registry: existing name skips; fresh fails capacity.
	doc := `{"mcpServers": {"aa": {"command": "x"}, "brand-new": {"command": "x"}}}`
	code, result, body := postImport(t, daemon.URL, importBody(doc, "", ""))
	if code != http.StatusOK {
		t.Fatalf("import: %d %s", code, body)
	}
	if string(result.Results[0].Status) != mcpimport.StatusSkipped || result.Results[0].Reason == nil || string(*result.Results[0].Reason) != mcpimport.ReasonDuplicate {
		t.Fatalf("existing name should skip duplicate even at capacity: %s", body)
	}
	if string(result.Results[1].Status) != mcpimport.StatusFailed || result.Results[1].Reason == nil || string(*result.Results[1].Reason) != mcpimport.ReasonCapacity {
		t.Fatalf("fresh should fail capacity: %s", body)
	}
}

func TestImportHandlerIOErrorsKeepReport(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based injection does not work as root")
	}
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := startDaemon(t, srv)
	dirs := lastTestDirs
	if err := os.Chmod(dirs.MCPs, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dirs.MCPs, 0o755) })
	doc := `{"mcpServers": {"one": {"command": "x"}, "two": {"command": "y"}}}`
	code, result, body := postImport(t, daemon.URL, importBody(doc, "", ""))
	if code != http.StatusOK {
		t.Fatalf("io failures must keep the per-entry report: %d %s", code, body)
	}
	for i, r := range result.Results {
		if string(r.Status) != mcpimport.StatusFailed || r.Reason == nil || string(*r.Reason) != mcpimport.ReasonIO {
			t.Fatalf("entry %d should be io_error: %s", i, body)
		}
	}
	if err := os.Chmod(dirs.MCPs, 0o755); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.reg.Snapshot().MCPs); n != 0 {
		t.Fatalf("nothing should have been created, got %d", n)
	}
}

func TestImportHandlerReplaceMode(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	if _, err := srv.reg.CreateMCP("target", registry.MCPSpec{Command: "old"}); err != nil {
		t.Fatal(err)
	}
	daemon := startDaemon(t, srv)

	// Valid single entry replaces transports wholesale, name unchanged.
	doc := `{"mcpServers": {"whatever-key": {"command": "new", "args": ["--v"]}}}`
	code, result, body := postImport(t, daemon.URL, importBody(doc, "", "target"))
	if code != http.StatusOK || len(result.Results) != 1 || string(result.Results[0].Status) != mcpimport.StatusCreated {
		t.Fatalf("replace: %d %s", code, body)
	}
	spec, ok := srv.reg.Snapshot().MCPs["target"]
	if !ok || spec.Command != "new" || len(spec.Args) != 1 {
		t.Fatalf("target not replaced: %+v", spec)
	}
	if _, still := srv.reg.Snapshot().MCPs["whatever-key"]; still {
		t.Fatal("replace must not create additional servers")
	}

	// Multi-entry rejects with the target untouched.
	before := srv.reg.Snapshot().MCPs["target"].Command
	multi := `{"mcpServers": {"a": {"command": "x"}, "b": {"command": "y"}}}`
	if code, _, _ := postImport(t, daemon.URL, importBody(multi, "", "target")); code != http.StatusBadRequest {
		t.Fatalf("multi-entry replace should 400: %d", code)
	}
	if srv.reg.Snapshot().MCPs["target"].Command != before {
		t.Fatal("target changed on rejected replace")
	}

	// An entry that would skip rejects; unknown target 404s.
	if code, _, _ := postImport(t, daemon.URL, importBody(`{"command": "c", "disabled": true}`, "", "target")); code != http.StatusBadRequest {
		t.Fatalf("skip-entry replace should 400: %d", code)
	}
	if code, _, _ := postImport(t, daemon.URL, importBody(`{"command": "c"}`, "", "ghost")); code != http.StatusNotFound {
		t.Fatalf("unknown replace target should 404: %d", code)
	}
}

func TestImportHandlerReplaceModeValidation(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	if _, err := srv.reg.CreateMCP("target", registry.MCPSpec{Command: "old"}); err != nil {
		t.Fatal(err)
	}
	daemon := startDaemon(t, srv)
	// Explicitly empty replaceName is a 400, never silent create mode.
	code, _, body := postImport(t, daemon.URL, importBody(`{"mcpServers": {"sneaky": {"command": "x"}}}`, "", " "))
	if code != http.StatusBadRequest {
		t.Fatalf("empty replaceName: %d %s", code, body)
	}
	if _, ok := srv.reg.Snapshot().MCPs["sneaky"]; ok {
		t.Fatal("empty replaceName must not fall through to create")
	}
	// Null values fail BEFORE touching the replace target: bytes and
	// snapshot both preserved.
	targetPath := srv.reg.Snapshot().MCPs["target"].Path
	before, _ := os.ReadFile(targetPath)
	code, _, body = postImport(t, daemon.URL, importBody(`{"mcpServers": {"t": {"command": "c", "env": {"TOKEN": null}}}}`, "", "target"))
	if code != http.StatusBadRequest {
		t.Fatalf("null env replace: %d %s", code, body)
	}
	after, _ := os.ReadFile(targetPath)
	if string(before) != string(after) || srv.reg.Snapshot().MCPs["target"].Command != "old" {
		t.Fatal("rejected replace must preserve the target (bytes and snapshot)")
	}
}

func TestImportHandlerUnconfiguredPreflight(t *testing.T) {
	// A registry never LoadAll'd: document-level 500 before any entry.
	dirs := lastTestDirs
	fresh := registry.New()
	srv := New(nil, providers.NewClient(), providers.NewCatalog(), newFakeChatService(), nil, nil, nil, fresh, nil, nil, nil, "test")
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	_ = dirs
	code, _, body := postImport(t, daemon.URL, importBody(`{"mcpServers": {"a": {"command": "x"}}}`, "", ""))
	if code != http.StatusInternalServerError || !strings.Contains(body, "unconfigured") {
		t.Fatalf("unconfigured registry: %d %s", code, body)
	}
}

func TestImportHandlerLastSlotAndOrdering(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	dirs := lastTestDirs
	// Fill to capacity-1, then import two entries whose keys collide on
	// one slug: the first fills the last slot, the second must SKIP as
	// duplicate (not capacity).
	for i := 0; i < 127; i++ {
		stem := string(rune('a'+i%26)) + string(rune('a'+i/26))
		if err := os.WriteFile(filepath.Join(dirs.MCPs, stem+".yaml"), []byte("command: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, errs := srv.reg.LoadAll(dirs); len(errs) > 0 {
		t.Fatal(errs)
	}
	daemon := startDaemon(t, srv)
	doc := `{"mcpServers": {"Filesystem (local)": {"command": "x"}, "filesystem-local": {"command": "x"}}}`
	code, result, body := postImport(t, daemon.URL, importBody(doc, "", ""))
	if code != http.StatusOK {
		t.Fatalf("last-slot import: %d %s", code, body)
	}
	if string(result.Results[0].Status) != mcpimport.StatusCreated || *result.Results[0].Name != "filesystem-local" {
		t.Fatalf("first colliding key should create: %s", body)
	}
	if string(result.Results[1].Status) != mcpimport.StatusSkipped || result.Results[1].Reason == nil || string(*result.Results[1].Reason) != mcpimport.ReasonDuplicate {
		t.Fatalf("second colliding key must skip duplicate even at capacity: %s", body)
	}
	// Failure-then-valid on a FRESH registry (the one above is full):
	// a broken entry first, a valid entry second — both reported, the
	// valid one created.
	srv2 := newTestServer(nil, providers.NewCatalog())
	daemon2 := startDaemon(t, srv2)
	code, result, body = postImport(t, daemon2.URL, importBody(`{"mcpServers": {"broken": {"args": ["x"]}, "works-later": {"command": "y"}}}`, "", ""))
	if code != http.StatusOK {
		t.Fatalf("failure-then-valid: %d %s", code, body)
	}
	if string(result.Results[0].Status) != mcpimport.StatusFailed || string(result.Results[1].Status) != mcpimport.StatusCreated {
		t.Fatalf("order/outcomes wrong: %s", body)
	}
}

func TestImportGuardRegressions(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	daemon := startDaemon(t, srv)
	// Foreign Host on the import route.
	req, _ := http.NewRequest("POST", daemon.URL+"/api/mcps/import", strings.NewReader(importBody(`{"mcpServers": {}}`, "", "")))
	req.Host = "rebind.attacker.example"
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-host import: %d", res.StatusCode)
	}
	// Cross-origin mutation without same-host Origin.
	req, _ = http.NewRequest("POST", daemon.URL+"/api/mcps/import", strings.NewReader(importBody(`{"mcpServers": {}}`, "", "")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin import: %d", res.StatusCode)
	}
}

// --- helpers -----------------------------------------------------------------

func startDaemon(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	daemon := httptest.NewServer(srv.Routes())
	t.Cleanup(daemon.Close)
	return daemon
}

func probeOK(tool string) mcpclient.ProbeResult {
	return mcpclient.ProbeResult{Results: []mcpclient.TransportResult{{Transport: "stdio", Status: "ok", Tools: []mcpclient.Tool{{Name: tool}}}}}
}

func TestMcpEnabledHandlerRoundTrip(t *testing.T) {
	srv := newTestServerWithProber(nil, providers.NewCatalog(), &fakeProber{result: probeOK("echo")})
	daemon := startDaemon(t, srv)
	// Default create: enabled; response and snapshot agree.
	code, body := mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "t1", "stdio": map[string]any{"command": "x"}})
	if code != http.StatusCreated || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("create default: %d %s", code, body)
	}
	// Explicit-false create; list carries it.
	if code, _ := mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "t2", "stdio": map[string]any{"command": "x"}, "enabled": false}); code != http.StatusCreated {
		t.Fatalf("create disabled: %d", code)
	}
	_, listBody := mustDo(t, daemon, "GET", "/api/mcps", nil)
	if !strings.Contains(listBody, `"name":"t2"`) || !strings.Contains(listBody, `"enabled":false`) {
		t.Fatalf("list disabled: %s", listBody)
	}
	// Probe STILL works on a disabled server (fake prober returns ok).
	code, body = mustDo(t, daemon, "POST", "/api/mcps/t2/test", map[string]any{})
	if code != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("probe disabled: %d %s", code, body)
	}
	// Rename carries explicit false; absent enabled on full replace re-enables.
	code, body = mustDo(t, daemon, "PATCH", "/api/mcps/t2", map[string]any{"name": "t2r", "stdio": map[string]any{"command": "x"}, "enabled": false})
	if code != http.StatusOK || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("rename disabled: %d %s", code, body)
	}
	code, body = mustDo(t, daemon, "PATCH", "/api/mcps/t2r", map[string]any{"stdio": map[string]any{"command": "x"}})
	if code != http.StatusOK || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("absent re-enables: %d %s", code, body)
	}
	// Imported servers are enabled.
	code, _, body = postImport(t, daemon.URL, importBody(`{"mcpServers": {"imp": {"command": "x"}}}`, "", ""))
	if code != http.StatusOK || !strings.Contains(body, `"name":"imp"`) || !strings.Contains(body, `"status":"created"`) {
		t.Fatalf("import: %d %s", code, body)
	}
	if srv.reg.Snapshot().MCPs["imp"].Disabled {
		t.Fatal("imports must create enabled servers")
	}
}

func TestMarketplaceHandlerFlows(t *testing.T) {
	srv := newTestServer(nil, providers.NewCatalog())
	store, err := skillhub.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSkillHub(fakeIndex{}, fakeFetcher{}, store)
	daemon := startDaemon(t, srv)

	// Search proxies with results.
	code, body := mustDo(t, daemon, "GET", "/api/skills/marketplace/search?q=pdf", nil)
	if code != http.StatusOK || !strings.Contains(body, "anthropics/skills/pdf") {
		t.Fatalf("search: %d %s", code, body)
	}
	// Index down is typed 502.
	srv2 := newTestServer(nil, providers.NewCatalog())
	srv2.SetSkillHub(fakeIndex{down: true}, fakeFetcher{}, mustStore(t))
	d2 := startDaemon(t, srv2)
	code, body = mustDo(t, d2, "GET", "/api/skills/marketplace/search?q=pdf", nil)
	if code != http.StatusBadGateway || !strings.Contains(body, "index_unreachable") {
		t.Fatalf("index down: %d %s", code, body)
	}

	// Preview → capability → install (byte-bound happy path).
	code, body = mustDo(t, daemon, "POST", "/api/skills/marketplace/preview", map[string]any{"source": "o/r"})
	if code != http.StatusOK || !strings.Contains(body, "PDF Kit") || !strings.Contains(body, `"capability"`) {
		t.Fatalf("preview: %d %s", code, body)
	}
	var parsed struct {
		Capability string `json:"capability"`
		Commit     string `json:"commit"`
	}
	_ = json.Unmarshal([]byte(body), &parsed)
	code, body = mustDo(t, daemon, "POST", "/api/skills/marketplace/install", map[string]any{
		"capability": parsed.Capability, "skills": []string{"PDF Kit"},
	})
	if code != http.StatusOK || !strings.Contains(body, `"status":"created"`) || !strings.Contains(body, "pdf-kit") {
		t.Fatalf("install: %d %s", code, body)
	}
	if s := srv.reg.Snapshot().Skills["PDF Kit"]; s.Origin != registry.OriginMarketplace || s.Source != "o/r" {
		t.Fatalf("origin wrong: %+v", s)
	}
	// Capability replay is one-shot: 404.
	code, _ = mustDo(t, daemon, "POST", "/api/skills/marketplace/install", map[string]any{
		"capability": parsed.Capability, "skills": []string{"PDF Kit"},
	})
	if code != http.StatusNotFound {
		t.Fatalf("replay: %d", code)
	}
	// A FRESH preview of the same repo → duplicate outcome (skill exists).
	_, pbody := mustDo(t, daemon, "POST", "/api/skills/marketplace/preview", map[string]any{"source": "o/r"})
	_ = json.Unmarshal([]byte(pbody), &parsed)
	code, body = mustDo(t, daemon, "POST", "/api/skills/marketplace/install", map[string]any{
		"capability": parsed.Capability, "skills": []string{"PDF Kit"},
	})
	if !strings.Contains(body, `"status":"skipped"`) || !strings.Contains(body, "duplicate") {
		t.Fatalf("duplicate: %d %s", code, body)
	}
	// Subset authorization: an unrecorded name rejects before any write.
	_, abody := mustDo(t, daemon, "POST", "/api/skills/marketplace/preview", map[string]any{"source": "o/r"})
	_ = json.Unmarshal([]byte(abody), &parsed)
	code, body = mustDo(t, daemon, "POST", "/api/skills/marketplace/install", map[string]any{
		"capability": parsed.Capability, "skills": []string{"Never Previewed"},
	})
	if code != http.StatusBadRequest || !strings.Contains(body, "was not in the preview") {
		t.Fatalf("subset auth: %d %s", code, body)
	}
	// Expired-capability changed-upstream: capability whose digests differ
	// from the fetcher's content → typed 502 changed_upstream.
	var changed parsedCap
	_, cbody := mustDo(t, daemon, "POST", "/api/skills/marketplace/preview", map[string]any{"source": "o/r"})
	_ = json.Unmarshal([]byte(cbody), &changed)
	srv.hub.fetch = changedFetcher{}
	code, body = mustDo(t, daemon, "POST", "/api/skills/marketplace/install", map[string]any{
		"capability": changed.Capability, "skills": []string{"PDF Kit"},
	})
	if code != http.StatusBadGateway || !strings.Contains(body, "changed_upstream") {
		t.Fatalf("changed upstream: %d %s", code, body)
	}

	// Archive import: multipart preview → commit one-shot.
	importSrv := newTestServer(nil, providers.NewCatalog())
	store3 := mustStore(t)
	importSrv.SetSkillHub(fakeIndex{}, fakeFetcher{}, store3)
	d3 := startDaemon(t, importSrv)
	zipBody := buildZip(t)
	req, _ := http.NewRequest("POST", d3.URL+"/api/skills/import/preview", bytes.NewReader(zipBody))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	var mp bytes.Buffer
	mp.WriteString("--BOUND\r\nContent-Disposition: form-data; name=\"archive\"; filename=\"x.skill\"\r\nContent-Type: application/zip\r\n\r\n")
	mp.Write(zipBody)
	mp.WriteString("\r\n--BOUND--\r\n")
	req, _ = http.NewRequest("POST", d3.URL+"/api/skills/import/preview", bytes.NewReader(mp.Bytes()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	res, err := d3.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var previewBody struct {
		StagingID string                `json:"stagingId"`
		Skill     struct{ Name string } `json:"skill"`
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("archive preview: %d %s", res.StatusCode, buf.String())
	}
	_ = json.Unmarshal(buf.Bytes(), &previewBody)
	if previewBody.Skill.Name != "PDF Kit" {
		t.Fatalf("archive preview skill: %s", buf.String())
	}
	code, body = mustDo(t, d3, "POST", "/api/skills/import/commit", map[string]any{"stagingId": previewBody.StagingID})
	if code != http.StatusOK || !strings.Contains(body, `"origin":"import"`) {
		t.Fatalf("archive commit: %d %s", code, body)
	}
	// Replay is expired.
	code, _ = mustDo(t, d3, "POST", "/api/skills/import/commit", map[string]any{"stagingId": previewBody.StagingID})
	if code != http.StatusNotFound {
		t.Fatalf("replay: %d", code)
	}

	// Multipart: an extra text part rejects.
	var mp2 bytes.Buffer
	mp2.WriteString("--BOUND\r\nContent-Disposition: form-data; name=\"extra\"\r\n\r\nnope\r\n--BOUND\r\nContent-Disposition: form-data; name=\"archive\"; filename=\"x.skill\"\r\nContent-Type: application/zip\r\n\r\n")
	mp2.Write(zipBody)
	mp2.WriteString("\r\n--BOUND--\r\n")
	req, _ = http.NewRequest("POST", d3.URL+"/api/skills/import/preview", bytes.NewReader(mp2.Bytes()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	res, err = d3.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("extra part: %d", res.StatusCode)
	}
	// Oversized body (over the 10 MiB contract bound) is 413.
	var mp3 bytes.Buffer
	mp3.WriteString("--BOUND\r\nContent-Disposition: form-data; name=\"archive\"; filename=\"big.skill\"\r\n\r\n")
	mp3.Write(bytes.Repeat([]byte("z"), 10<<20+64))
	mp3.WriteString("\r\n--BOUND--\r\n")
	req, _ = http.NewRequest("POST", d3.URL+"/api/skills/import/preview", bytes.NewReader(mp3.Bytes()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	res, err = d3.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: %d", res.StatusCode)
	}

	// Guard: hostile origin on the multipart route; text/plain form still 415.
	req, _ = http.NewRequest("POST", d3.URL+"/api/skills/import/preview", bytes.NewReader(mp.Bytes()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	req.Header.Set("Origin", "http://evil.example")
	res, err = d3.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("hostile origin multipart: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", d3.URL+"/api/skills/marketplace/preview", strings.NewReader("source=x"))
	req.Header.Set("Content-Type", "text/plain")
	res, err = d3.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("plain form post should stay 415: %d", res.StatusCode)
	}
}

func mustStore(t *testing.T) *skillhub.Store {
	t.Helper()
	st, err := skillhub.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

type fakeIndex struct{ down bool }

func (f fakeIndex) Search(ctx context.Context, query string) ([]skillhub.IndexSkill, error) {
	if f.down {
		return nil, skillhub.ErrIndexUnreachable
	}
	return []skillhub.IndexSkill{{ID: "anthropics/skills/pdf", Source: "anthropics/skills", Name: "pdf", Installs: 234146}}, nil
}

type fakeFetcher struct{}

func (f fakeFetcher) Preview(ctx context.Context, source string) (*skillhub.RepoPreview, error) {
	return &skillhub.RepoPreview{
		Source: "o/r", Commit: "abc123", License: "LICENSE",
		Skills: []skillhub.PreviewSkill{{
			Name: "PDF Kit", Description: "PDF tooling.", Body: "# PDF Kit\n",
			Files: []skillhub.FileDigest{{Path: "SKILL.md", Size: int64(len(sampleBody())), Sum: sumHex([]byte(sampleBody()))}},
			Root:  "skills/pdf",
		}},
	}, nil
}

func (f fakeFetcher) ExtractCommit(ctx context.Context, source, commit string) (map[string][]registry.SkillFile, map[string]map[string]skillhub.FileDigest, error) {
	files := []registry.SkillFile{{Path: "SKILL.md", Mode: 0o644, Data: []byte(sampleBody())}}
	digests := map[string]skillhub.FileDigest{"SKILL.md": {Path: "SKILL.md", Size: int64(len(sampleBody())), Sum: sumHex([]byte(sampleBody()))}}
	return map[string][]registry.SkillFile{"PDF Kit": files}, map[string]map[string]skillhub.FileDigest{"PDF Kit": digests}, nil
}

func sampleBody() string {
	return "---\nname: PDF Kit\ndescription: PDF tooling.\n---\n# PDF Kit\nBody text.\n"
}

type parsedCap struct {
	Capability string `json:"capability"`
	Commit     string `json:"commit"`
}

// changedFetcher returns content whose digests differ from the recorded
// preview (upstream change under the same commit).
type changedFetcher struct{}

func (changedFetcher) Preview(ctx context.Context, source string) (*skillhub.RepoPreview, error) {
	return fakeFetcher{}.Preview(ctx, source)
}

func (changedFetcher) ExtractCommit(ctx context.Context, source, commit string) (map[string][]registry.SkillFile, map[string]map[string]skillhub.FileDigest, error) {
	changed := []byte(strings.Replace(sampleBody(), "Body text.", "Changed upstream!", 1))
	files := map[string][]registry.SkillFile{"PDF Kit": {{Path: "SKILL.md", Mode: 0o644, Data: changed}}}
	digests := map[string]map[string]skillhub.FileDigest{"PDF Kit": {"SKILL.md": {Path: "SKILL.md", Size: int64(len(changed)), Sum: sumHex(changed)}}}
	return files, digests, nil
}

func sumHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func buildZip(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "SKILL.md", Method: zip.Deflate})
	_, _ = w.Write([]byte(sampleBody()))
	_ = zw.Close()
	return buf.Bytes()
}

// A successful probe feeds the resource index (the daemon's MCP-tool
// lane — phase-5 final-review P1); a failed probe does not.
func TestMcpProbeFeedsResourceIndex(t *testing.T) {
	t.Parallel()
	var fed []mcpclient.Tool
	var fedServer string
	done := make(chan struct{})
	srv := newTestServerWithProber(nil, nil, &scriptedProber{result: mcpclient.ProbeResult{Results: []mcpclient.TransportResult{{
		Transport: "stdio", Status: mcpclient.StatusOK,
		Tools: []mcpclient.Tool{{Name: "web_search", Description: "search the web"}},
	}}}})
	srv.SetMCPToolIndexer(func(server string, tools []mcpclient.Tool) {
		fedServer, fed = server, tools
		close(done)
	})
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	mustDo(t, daemon, "POST", "/api/mcps", map[string]any{"name": "ddg", "stdio": map[string]any{"command": "x"}})
	code, _ := mustDo(t, daemon, "POST", "/api/mcps/ddg/test", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("probe: %d", code)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("indexer never fed")
	}
	if fedServer != "ddg" || len(fed) != 1 || fed[0].Name != "web_search" {
		t.Fatalf("fed: %s %+v", fedServer, fed)
	}
}

// scriptedProber returns a fixed result (fakeProber has no tools field).
type scriptedProber struct {
	result mcpclient.ProbeResult
	spec   registry.MCPSpec
}

func (p *scriptedProber) Test(_ context.Context, spec registry.MCPSpec) mcpclient.ProbeResult {
	p.spec = spec
	return p.result
}
