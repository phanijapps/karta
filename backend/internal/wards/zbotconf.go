package wards

// The ward's .zbot/config.yaml: identity + state. USER-OWNED — the
// daemon reads it freely but writes ONLY last_used (touched on ward
// entry, best-effort). Everything else (name, description, rosters)
// belongs to the user.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TouchLastUsed stamps .zbot/config.yaml's last_used (best-effort).
// SURGICAL: only the exact top-level `last_used:` key's scalar is
// replaced — similarly prefixed keys (`last_used_note:`) are untouched
// and any trailing inline comment on the line is preserved. An absent
// file gets the starter + stamp; any other read error leaves the file
// alone. Never fails the caller.
func TouchLastUsed(wardDir string) {
	path := filepath.Join(wardDir, ".zbot", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return // unreadable for a non-absence reason: leave it alone
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		if werr := os.WriteFile(path, []byte(stampLine(zbotConfigStarter, time.Now())), 0o644); werr != nil {
			return // best-effort by contract
		}
		return
	}
	stamped := stampLine(string(raw), time.Now())
	if stamped == string(raw) && !hasLastUsedKey(string(raw)) {
		// No exact last_used key (prefixed keys do NOT count): append.
		stamped += lastUsedLine(time.Now())
		if !strings.HasSuffix(stamped, "\n") {
			stamped += "\n"
		}
	}
	if werr := os.WriteFile(path, []byte(stamped), 0o644); werr != nil {
		return // best-effort by contract
	}
}

func lastUsedLine(t time.Time) string {
	return "last_used: " + t.UTC().Format(time.RFC3339)
}

// stampLine replaces the exact `last_used:` scalar in the YAML text,
// preserving the rest byte-for-byte (inline comment kept).
func stampLine(text string, t time.Time) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		rest := line[:len(line)-len(trimmed)]
		if trimmed == line {
			rest = ""
		}
		if key, after, ok := strings.Cut(trimmed, ":"); ok && key == "last_used" {
			// Preserve an inline comment after the scalar.
			comment := ""
			if ci := strings.Index(after, "#"); ci >= 0 {
				comment = " " + strings.TrimRight(after[ci:], " \t\r\n")
			}
			b.WriteString(rest + lastUsedLine(t) + comment + "\n")
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// hasLastUsedKey reports whether the YAML text carries an exact
// top-level last_used key (the append gate — prefixed keys don't
// count).
func hasLastUsedKey(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if key, _, ok := strings.Cut(trimmed, ":"); ok && key == "last_used" {
			return true
		}
	}
	return false
}
