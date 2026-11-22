package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"zbot/backend/distiller"
	"zbot/backend/internal/server/apigen"
)

// Distillation settings: daemon-managed ~/.zbot/config/distillation.json,
// read fresh each call (the distiller re-reads it each pass too).
// Provider is validated against the configured provider list.

func distillationPath() string {
	dir := os.Getenv("ZBOT_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".zbot")
	}
	return filepath.Join(dir, "config", "distillation.json")
}

// GetDistillation — GetDistillation serves the distillation settings (contract GET).
func (s *Server) GetDistillation(w http.ResponseWriter, r *http.Request) {
	settings := map[string]any{}
	if raw, err := os.ReadFile(distillationPath()); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	// Seed the effective prompt when absent (the embedded default from
	// the distiller's own package is mirrored here for the settings
	// surface; provider/model stay empty until the user chooses).
	if _, ok := settings["prompt"]; !ok {
		settings["prompt"] = distiller.DefaultPrompt()
	}
	writeJSON(w, http.StatusOK, settings)
}

// PutDistillation — PutDistillation replaces the distillation settings (contract PUT).
func (s *Server) PutDistillation(w http.ResponseWriter, r *http.Request) {
	var body apigen.DistillationSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	if body.Provider == nil || *body.Provider == "" {
		writeError(w, http.StatusBadRequest, "empty_provider", "provider must be set")
		return
	}
	// Validate provider against the configured set.
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
	if body.Model == nil || *body.Model == "" {
		writeError(w, http.StatusBadRequest, "empty_model", "model must be set")
		return
	}
	// Partial update: read-modify-write so omitted fields persist.
	settings := map[string]any{}
	if raw, err := os.ReadFile(distillationPath()); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	settings["provider"] = *body.Provider
	settings["model"] = *body.Model
	if body.Prompt != nil && *body.Prompt != "" {
		settings["prompt"] = *body.Prompt
	}
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal", err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(distillationPath()), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "mkdir", err.Error())
		return
	}
	// Atomic write (temp + rename — no torn reads).
	tmp := distillationPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	if err := os.Rename(tmp, distillationPath()); err != nil {
		writeError(w, http.StatusInternalServerError, "rename", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
