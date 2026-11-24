package wards

// The ward's .zbot/config.yaml: identity + state. USER-OWNED — the
// daemon reads it freely but writes ONLY last_used (touched on ward
// entry, best-effort). Everything else (name, description, rosters)
// belongs to the user.

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ZbotConfig is the parsed .zbot/config.yaml (defaults when absent).
type ZbotConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	LastUsed    string   `yaml:"last_used"`
	Agents      []string `yaml:"agents"`
	Skills      []string `yaml:"skills"`
}

// zbotConfigStarter is the write-once starter seeded by MigrateV2 and
// the archetype templates.
const zbotConfigStarter = `# Ward identity + state. USER-OWNED: Karta touches only last_used
# (on ward entry); everything else is yours.
name: ""            # defaults to the ward directory name
description: ""     # one line: what this ward is for
last_used: ""       # RFC3339, touched by Karta on ward entry
agents: []          # ward-scoped agent seeds (future surface)
skills: []          # ward-scoped skill seeds (future surface)
`

// ReadZbotConfig reads the ward's .zbot/config.yaml (zero-value
// defaults when absent or unparseable — identity is advisory state,
// never a failure).
func ReadZbotConfig(wardDir string) ZbotConfig {
	var cfg ZbotConfig
	raw, err := os.ReadFile(filepath.Join(wardDir, ".zbot", "config.yaml"))
	if err != nil {
		return ZbotConfig{}
	}
	if uerr := yaml.Unmarshal(raw, &cfg); uerr != nil {
		cfg = ZbotConfig{} // unparseable = defaults; user-owned file
	}
	return cfg
}

// TouchLastUsed stamps .zbot/config.yaml's last_used (best-effort:
// parse, update the one field, rewrite; absent file → starter +
// stamp). Never fails the caller.
func TouchLastUsed(wardDir string) {
	path := filepath.Join(wardDir, ".zbot", "config.yaml")
	raw, err := os.ReadFile(path)
	text := zbotConfigStarter
	if err == nil && len(raw) > 0 {
		text = string(raw)
	}
	stamp := "last_used: " + time.Now().UTC().Format(time.RFC3339)
	// Replace the last_used line in place (comment-preserving); append
	// when absent.
	lines := splitLines(text)
	found := false
	for i, l := range lines {
		if len(l) >= 9 && l[:9] == "last_used" {
			lines[i] = stamp
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, stamp)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, joinLines(lines), 0o644); err != nil {
		return // best-effort by contract
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func joinLines(lines []string) []byte {
	out := make([]byte, 0, 256)
	for _, l := range lines {
		out = append(out, l...)
		out = append(out, '\n')
	}
	return out
}
