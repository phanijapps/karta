// Package config resolves, bootstraps, and loads z-Bot's filesystem
// control plane. The home root defaults to ~/.zbot and can be relocated
// whole-root via the ZBOT_HOME environment variable or an explicit flag
// override (wired to zbotd's --home flag when the entrypoint lands).
package config

import (
	"os"
	"path/filepath"
)

// Subdirs are the control-plane registry directories under <home>/config/.
var Subdirs = []string{"prompts", "skills", "hooks", "mcps", "schedules", "providers"}

// ResolveHome resolves the z-Bot home root with strict precedence: a
// non-empty flagOverride, then the ZBOT_HOME environment variable, then
// ~/.zbot. The winner relocates the entire control plane (config.yaml,
// config/**, data/) — there are no partial overrides. The ward vault is
// NOT part of the home: it is user data outside ~/.zbot
// (wards.ResolveVaultRoot).
func ResolveHome(flagOverride string) (string, error) {
	for _, candidate := range []string{flagOverride, os.Getenv("ZBOT_HOME")} {
		if candidate != "" {
			return filepath.Abs(candidate)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zbot"), nil
}

// Paths derives every control-plane path from a resolved home so no call
// site re-derives them.
type Paths struct {
	Home         string
	ConfigFile   string
	DataDir      string
	ProvidersDir string
	AgentsDir    string // ~/.zbot/agents — the agent roster's top-level home
}

// NewPaths derives Paths for a resolved home.
func NewPaths(home string) Paths {
	return Paths{
		Home:         home,
		ConfigFile:   filepath.Join(home, "config.yaml"),
		DataDir:      filepath.Join(home, "data"),
		ProvidersDir: filepath.Join(home, "config", "providers"),
		AgentsDir:    filepath.Join(home, "agents"),
	}
}

// ConfigDir returns the path of one config subdirectory (a Subdirs entry).
func (p Paths) ConfigDir(kind string) string {
	return filepath.Join(p.Home, "config", kind)
}

// AllConfigDirs returns every config subdirectory path in canonical order.
func (p Paths) AllConfigDirs() []string {
	dirs := make([]string, 0, len(Subdirs))
	for _, kind := range Subdirs {
		dirs = append(dirs, p.ConfigDir(kind))
	}
	return dirs
}
