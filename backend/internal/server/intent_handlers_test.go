package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zbot/backend/internal/config"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/intent"
	"zbot/backend/internal/providers"
)

// fakeTester scripts the probe seam.
type fakeTester struct {
	estimate int
	result   intent.TestResult
	goal     string
}

func (f *fakeTester) TestAnalysis(_ context.Context, goal string) intent.TestResult {
	f.goal = goal
	return f.result
}

func (f *fakeTester) PacketEstimate(context.Context) int { return f.estimate }

func newIntentDaemon(t *testing.T) (*httptest.Server, *fakeTester, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ZBOT_HOME", home)
	paths := config.NewPaths(home)
	if err := os.MkdirAll(paths.ProvidersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Providers = []config.ProviderConfig{
		{Name: "zai", Type: "openai-compatible", BaseURL: "https://api.z.ai/api/paas/v4"},
		{Name: "ollama", Type: "openai-compatible", BaseURL: "http://localhost:11434/v1"},
	}
	store, err := providers.NewStore(paths, cfg, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	catalog := providers.NewCatalog()
	catalog.Set("ollama", []providers.Model{{ID: "glm5", ContextWindow: 1048576}, {ID: "mystery"}})
	tester := &fakeTester{estimate: 2000}
	s := New(store, providers.NewClient(), catalog, nil, nil, nil, nil, nil, nil, nil, nil, "test")
	s.SetIntentTester(tester)
	s.SetIntentEnabled(true)
	s.SetIntentConfigDir(paths.ConfigDir(""))
	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)
	return srv, tester, paths.ConfigDir("")
}

// doJSON sends one JSON request and returns the response.
func doJSON(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json") // the mutation guard requires it, body or not
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestIntentAnalysisHandlers(t *testing.T) {
	srv, tester, configDir := newIntentDaemon(t)

	t.Run("GET reports nothing selected and the enabled state", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/api/intent-analysis")
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("GET: %v %v", err, res.StatusCode)
		}
		defer res.Body.Close()
		var body struct {
			Settings  map[string]any `json:"settings"`
			Effective struct {
				Model         string `json:"model"`
				Enabled       bool   `json:"enabled"`
				OutputReserve int    `json:"output_reserve"`
				PacketTokens  int    `json:"packet_tokens"`
				Trimming      bool   `json:"trimming"`
			} `json:"effective"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Effective.Model != "" || !body.Effective.Enabled || body.Effective.OutputReserve != 4000 || body.Effective.PacketTokens != 2000 || body.Effective.Trimming {
			t.Fatalf("effective: %+v", body.Effective)
		}
		if p, ok := body.Settings["provider"]; ok && p != "" {
			t.Fatalf("nothing selected yet: %v", body.Settings)
		}
	})

	t.Run("PUT rejects unknown provider and empty model", func(t *testing.T) {
		for _, tc := range []struct {
			body string
			code int
			id   string
		}{
			{`{"provider":"nope","model":"m"}`, http.StatusBadRequest, "unknown_provider"},
			{`{"provider":"zai"}`, http.StatusBadRequest, "empty_model"},
			{`{"model":"m"}`, http.StatusBadRequest, "empty_provider"},
		} {
			res := doJSON(t, http.MethodPut, srv.URL+"/api/intent-analysis", tc.body)
			var e struct{ Code string }
			_ = json.NewDecoder(res.Body).Decode(&e)
			res.Body.Close()
			if res.StatusCode != tc.code || e.Code != tc.id {
				t.Fatalf("%s: %d %s", tc.body, res.StatusCode, e.Code)
			}
		}
	})

	t.Run("PUT persists and derives the window through the chain", func(t *testing.T) {
		// ollama is a configured provider too for the discovered case.
		res := doJSON(t, http.MethodPut, srv.URL+"/api/intent-analysis", `{"provider":"zai","model":"glm-5.3-flash","deadline_seconds":30}`)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("PUT: %d", res.StatusCode)
		}
		var body struct {
			Settings struct {
				Provider        string `json:"provider"`
				Model           string `json:"model"`
				DeadlineSeconds int    `json:"deadline_seconds"`
				ReasoningEffort string `json:"reasoning_effort"`
			} `json:"settings"`
			Effective struct {
				Model         string `json:"model"`
				ContextWindow int    `json:"context_window"`
				WindowSource  string `json:"window_source"`
			} `json:"effective"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if body.Settings.Provider != "zai" || body.Settings.Model != "glm-5.3-flash" || body.Settings.DeadlineSeconds != 30 {
			t.Fatalf("settings: %+v", body.Settings)
		}
		if body.Effective.Model != "zai/glm-5.3-flash" || body.Effective.ContextWindow != 128000 || body.Effective.WindowSource != "table" {
			t.Fatalf("glm-5.3* resolves through the built-in table: %+v", body.Effective)
		}
		raw, err := os.ReadFile(filepath.Join(configDir, "intent-analysis.json"))
		if err != nil || !strings.Contains(string(raw), `"model": "glm-5.3-flash"`) || !strings.Contains(string(raw), `"deadline_seconds": 30`) {
			t.Fatalf("file: %v %s", err, raw)
		}
		// A discovered window beats the table; an unknown model falls back
		// and a packet that cannot fit shows the valve.
		tester.estimate = 999000 // a packet that cannot fit any real window
		res2 := doJSON(t, http.MethodPut, srv.URL+"/api/intent-analysis", `{"provider":"ollama","model":"mystery"}`)
		var eff struct {
			Effective struct {
				ContextWindow int    `json:"context_window"`
				WindowSource  string `json:"window_source"`
				Trimming      bool   `json:"trimming"`
			} `json:"effective"`
		}
		_ = json.NewDecoder(res2.Body).Decode(&eff)
		res2.Body.Close()
		if eff.Effective.ContextWindow != 32000 || eff.Effective.WindowSource != "fallback" || !eff.Effective.Trimming {
			t.Fatalf("unknown model falls back and the valve shows: %+v", eff.Effective)
		}
	})

	t.Run("Test runs the probe with the sample goal by default", func(t *testing.T) {
		tester.result = intent.TestResult{
			Status: domain.IntentStatusAnalyzed, Model: "zai/glm-5.3-flash", ContextWindow: 128000, WindowSource: domain.WindowTable,
			Attempts: 1, TokensIn: 1200, TokensOut: 180, DurationMS: 900, PacketTokens: 2000,
			Analysis: &domain.IntentAnalysisPayload{Status: domain.IntentStatusAnalyzed, PrimaryIntent: "probe", Complexity: domain.ComplexityM, StepBudget: 8},
		}
		res := doJSON(t, http.MethodPost, srv.URL+"/api/intent-analysis/test", "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("test: %d", res.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if out["status"] != "analyzed" || out["tokens_in"] != float64(1200) || out["tokens_out"] != float64(180) || out["duration_ms"] != float64(900) || out["attempts"] != float64(1) || out["window_source"] != "table" || out["trimmed"] != false {
			t.Fatalf("result: %v", out)
		}
		if a := out["analysis"].(map[string]any); a["primary_intent"] != "probe" || a["step_budget"] != float64(8) {
			t.Fatalf("analysis: %v", a)
		}
		if tester.goal != "" {
			t.Fatalf("an absent body means the sample goal (the wrapper substitutes it): %q", tester.goal)
		}
		// A custom goal rides the body; a failed probe is a 200 with the reason.
		res = doJSON(t, http.MethodPost, srv.URL+"/api/intent-analysis/test", `{"goal":"compare AMD and NVIDIA"}`)
		var custom map[string]any
		_ = json.NewDecoder(res.Body).Decode(&custom)
		res.Body.Close()
		if tester.goal != "compare AMD and NVIDIA" {
			t.Fatalf("custom goal: %q", tester.goal)
		}
		tester.result = intent.TestResult{Status: domain.IntentStatusFailed, Reason: "model call failed: down", Attempts: 2}
		res = doJSON(t, http.MethodPost, srv.URL+"/api/intent-analysis/test", "")
		out = nil
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if out["status"] != "failed" || out["reason"] != "model call failed: down" || out["attempts"] != float64(2) {
			t.Fatalf("failed probe: %v", out)
		}
	})

	t.Run("unwired daemon answers 503", func(t *testing.T) {
		s := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "test")
		w := httptest.NewRecorder()
		s.TestIntentAnalysis(w, httptest.NewRequest(http.MethodPost, "/api/intent-analysis/test", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("code %d", w.Code)
		}
	})
}
