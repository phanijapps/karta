package intent

// The intent-analyst agent binding (intent-analyst-agent S1): the
// analyst is a real registry agent — a seeded, user-owned spec whose
// rubric is referenced by prompt name and whose model can be
// overridden per-agent — and a host-run decision step (never a tool
// loop, never delegable). This file holds the binding and the
// daemon-managed settings file the Settings card writes; the wrapper
// composes both into the model precedence chain.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zbot/backend/internal/registry"
)

// AnalystAgentName is the reserved roster name of the intent analyst.
const AnalystAgentName = registry.SystemAgentIntentAnalyst

// intentPromptName is the seeded rubric the analyst references
// (config/prompts/intent-analysis.md; the embedded default mirrors it).
const intentPromptName = "intent-analysis"

// SettingsFileName is the daemon-managed settings file the card writes
// (Settings → Advanced → Intent Analysis): the model selection plus
// the advanced fields edited in the file. No token knobs.
const SettingsFileName = "intent-analysis.json"

// Settings are the intent-analyst's settings.
type Settings struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// ReasoningEffort is sent as reasoning_effort when set. Unset:
	// glm-5.3* gets "low" (the rig-preflight evidence), other models
	// nothing — their provider default.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// DeadlineSeconds bounds the whole decision (default 45).
	DeadlineSeconds int `json:"deadline_seconds,omitempty"`
	// MaxAttempts counts completions including correction retries
	// (default 3).
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// DefaultDeadline and DefaultAttempts are the shipped bounds.
const (
	DefaultDeadline = 45 * time.Second
	DefaultAttempts = 3
)

// Selected reports whether the card names a model.
func (s Settings) Selected() bool { return s.Provider != "" && s.Model != "" }

// Qualified returns the selection as "provider/model" ("" when none).
func (s Settings) Qualified() string {
	if !s.Selected() {
		return ""
	}
	return s.Provider + "/" + s.Model
}

// Deadline bounds the decision (the default when unset or absurd).
func (s Settings) Deadline() time.Duration {
	if s.DeadlineSeconds > 0 {
		return time.Duration(s.DeadlineSeconds) * time.Second
	}
	return DefaultDeadline
}

// Attempts bounds the completions (the default when unset or absurd).
func (s Settings) Attempts() int {
	if s.MaxAttempts > 0 {
		return s.MaxAttempts
	}
	return DefaultAttempts
}

// ReadSettings reads ~/.zbot/config/intent-analysis.json; an absent or
// broken file is the zero value (nothing selected, every default).
func ReadSettings(configDir string) Settings {
	var s Settings
	if raw, err := os.ReadFile(filepath.Join(configDir, SettingsFileName)); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	s.Provider = strings.TrimSpace(s.Provider)
	s.Model = strings.TrimSpace(s.Model)
	return s
}

// ApplyTo wires the settings' bounds and effort onto an analyzer.
func (s Settings) ApplyTo(a *Analyzer) {
	a.Deadline = s.Deadline()
	a.MaxAttempts = s.Attempts()
	a.ReasoningEffort = s.ReasoningEffort
}

// cutQualified splits "provider/model" (either may be empty).
func cutQualified(q string) (provider, model string) {
	p, m, _ := strings.Cut(q, "/")
	return strings.TrimSpace(p), strings.TrimSpace(m)
}
