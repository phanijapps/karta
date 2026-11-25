package server

// Intent-analysis settings (Settings → Advanced → Intent Analysis):
// the daemon-managed ~/.zbot/config/intent-analysis.json read fresh
// each call (the wrapper reads it per run too), plus the read-only
// derivation from the selected model, plus the Test analysis probe —
// the distillation card's pattern with the limits made visible.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/intent"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/server/apigen"
)

// IntentTester is the wrapper's probe seam: one real analysis with its
// cost, and the default packet's size estimate (no model call).
type IntentTester interface {
	TestAnalysis(ctx context.Context, goal string) intent.TestResult
	PacketEstimate(ctx context.Context) int
}

// SetIntentTester wires the analysis probe (the wrapped engine).
func (s *Server) SetIntentTester(t IntentTester) { s.intentTester = t }

// SetIntentEnabled records the config.yaml kill-switch for the card.
func (s *Server) SetIntentEnabled(on bool) { s.intentEnabled = on }

// SetIntentConfigDir names the config directory holding the settings
// file (unset = ZBOT_HOME / ~/.zbot, the distillation pattern).
func (s *Server) SetIntentConfigDir(dir string) { s.intentConfigDir = dir }

func (s *Server) intentSettingsPath() string {
	if s.intentConfigDir != "" {
		return filepath.Join(s.intentConfigDir, intent.SettingsFileName)
	}
	return filepath.Join(filepath.Dir(distillationPath()), intent.SettingsFileName)
}

// writeIntentSettings persists the settings atomically (temp + rename).
func (s *Server) writeIntentSettings(settings intent.Settings) error {
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	path := s.intentSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// intentEffective derives the read-only block for the selected model:
// the window + its source through the one resolution chain, and
// whether the valve would fire for a default packet.
func (s *Server) intentEffective(ctx context.Context, settings intent.Settings) apigen.IntentAnalysisEffective {
	eff := apigen.IntentAnalysisEffective{
		OutputReserve: intent.OutputReserve,
		Enabled:       s.intentEnabled,
		Trimming:      false,
	}
	if settings.Selected() {
		eff.Model = settings.Qualified()
		window, src := providers.ResolveWindow(s.catalog.ModelOrID(settings.Provider, settings.Model), s.store.Limits())
		source := apigen.IntentAnalysisEffectiveWindowSource(src)
		eff.ContextWindow = &window
		eff.WindowSource = &source
	}
	if s.intentTester != nil {
		estimate := s.intentTester.PacketEstimate(ctx)
		eff.PacketTokens = estimate
		if eff.ContextWindow != nil {
			eff.Trimming = estimate > *eff.ContextWindow-intent.OutputReserve
		}
	}
	return eff
}

// intentStatus reads the settings file fresh and derives the
// effective block for the selection.
func (s *Server) intentStatus(ctx context.Context) apigen.IntentAnalysisStatus {
	settings := intent.ReadSettings(filepath.Dir(s.intentSettingsPath()))
	view := apigen.IntentAnalysisSettings{}
	if settings.Provider != "" {
		view.Provider = &settings.Provider
	}
	if settings.Model != "" {
		view.Model = &settings.Model
	}
	if settings.ReasoningEffort != "" {
		view.ReasoningEffort = &settings.ReasoningEffort
	}
	if settings.DeadlineSeconds > 0 {
		view.DeadlineSeconds = &settings.DeadlineSeconds
	}
	if settings.MaxAttempts > 0 {
		view.MaxAttempts = &settings.MaxAttempts
	}
	return apigen.IntentAnalysisStatus{Settings: view, Effective: s.intentEffective(ctx, settings)}
}

// GetIntentAnalysis — GetIntentAnalysis serves the settings + derivation (contract GET).
func (s *Server) GetIntentAnalysis(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.intentStatus(r.Context()))
}

// PutIntentAnalysis — PutIntentAnalysis replaces the model selection (contract PUT).
func (s *Server) PutIntentAnalysis(w http.ResponseWriter, r *http.Request) {
	var body apigen.IntentAnalysisSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	if body.Provider == nil || *body.Provider == "" {
		writeError(w, http.StatusBadRequest, "empty_provider", "provider must be set")
		return
	}
	if body.Model == nil || *body.Model == "" {
		writeError(w, http.StatusBadRequest, "empty_model", "model must be set")
		return
	}
	known := false
	for _, p := range s.store.List() {
		if p.Name == *body.Provider {
			known = true
			break
		}
	}
	if !known {
		writeError(w, http.StatusBadRequest, "unknown_provider", "provider "+*body.Provider+" is not configured")
		return
	}
	// The contract's declared bounds, enforced at the handler (the file
	// is editable by hand, but the API never writes an absurd value).
	if msg := validateIntentSettings(body); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_settings", msg)
		return
	}
	// Partial update: read-modify-write so omitted fields persist.
	settings := intent.ReadSettings(filepath.Dir(s.intentSettingsPath()))
	settings.Provider, settings.Model = *body.Provider, *body.Model
	if body.ReasoningEffort != nil {
		settings.ReasoningEffort = *body.ReasoningEffort
	}
	if body.DeadlineSeconds != nil {
		settings.DeadlineSeconds = *body.DeadlineSeconds
	}
	if body.MaxAttempts != nil {
		settings.MaxAttempts = *body.MaxAttempts
	}
	if err := s.writeIntentSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.intentStatus(r.Context()))
}

// validateIntentSettings enforces the IntentAnalysisSettings schema's
// bounds ("" = valid); they mirror the contract's min/max/maxLength.
func validateIntentSettings(body apigen.IntentAnalysisSettings) string {
	if len(*body.Provider) > 128 {
		return "provider is too long (max 128)"
	}
	if len(*body.Model) > 256 {
		return "model is too long (max 256)"
	}
	if body.ReasoningEffort != nil && len(*body.ReasoningEffort) > 16 {
		return "reasoning_effort is too long (max 16)"
	}
	if body.DeadlineSeconds != nil && (*body.DeadlineSeconds < 1 || *body.DeadlineSeconds > 600) {
		return "deadline_seconds must be between 1 and 600"
	}
	if body.MaxAttempts != nil && (*body.MaxAttempts < 1 || *body.MaxAttempts > 5) {
		return "max_attempts must be between 1 and 5"
	}
	return ""
}

// TestIntentAnalysis — TestIntentAnalysis runs one real analysis (contract POST).
func (s *Server) TestIntentAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.intentTester == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "intent analysis is not wired")
		return
	}
	goal := ""
	if r.ContentLength != 0 {
		var body apigen.IntentAnalysisTestRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
			return
		}
		if body.Goal != nil {
			goal = *body.Goal
		}
	}
	res := s.intentTester.TestAnalysis(r.Context(), goal)
	out := apigen.IntentAnalysisTestResult{
		Status: apigen.IntentAnalysisTestResultStatus(res.Status), Attempts: res.Attempts,
		TokensIn: int(res.TokensIn), TokensOut: int(res.TokensOut), DurationMs: int(res.DurationMS),
		Trimmed: res.Trimmed, PacketTokens: res.PacketTokens,
	}
	if res.Reason != "" {
		out.Reason = &res.Reason
	}
	if res.Model != "" {
		out.Model = &res.Model
	}
	if res.ContextWindow > 0 {
		out.ContextWindow = &res.ContextWindow
		src := apigen.IntentAnalysisTestResultWindowSource(res.WindowSource)
		out.WindowSource = &src
	}
	if res.Analysis != nil {
		out.Analysis = contractAnalysisPayload(*res.Analysis)
	}
	writeJSON(w, http.StatusOK, out)
}

// contractAnalysisPayload maps the domain payload onto the contract
// type through their shared JSON shape (every optional field is a
// pointer in the generated type).
func contractAnalysisPayload(p domain.IntentAnalysisPayload) *apigen.IntentAnalysisPayload {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var out apigen.IntentAnalysisPayload
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return &out
}
