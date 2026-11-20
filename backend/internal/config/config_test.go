package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempHome(t *testing.T) (string, Paths) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "zbot-home")
	return home, NewPaths(home)
}

// writeConfig writes a config.yaml directly (creating the home dir), for
// tests that need a pre-made file without full bootstrap.
func writeConfig(t *testing.T, paths Paths, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveHomePrecedence(t *testing.T) {
	t.Run("default is ~/.zbot", func(t *testing.T) {
		t.Setenv("ZBOT_HOME", "") // neutralize a leaked environment override
		got, err := ResolveHome("")
		if err != nil {
			t.Fatalf("ResolveHome: %v", err)
		}
		home, _ := os.UserHomeDir()
		if want := filepath.Join(home, ".zbot"); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("env override", func(t *testing.T) {
		alt := filepath.Join(t.TempDir(), "alt")
		t.Setenv("ZBOT_HOME", alt)
		got, err := ResolveHome("")
		if err != nil {
			t.Fatalf("ResolveHome: %v", err)
		}
		if got != alt {
			t.Fatalf("got %s, want %s", got, alt)
		}
	})

	t.Run("flag beats env", func(t *testing.T) {
		alt := filepath.Join(t.TempDir(), "alt")
		flagHome := filepath.Join(t.TempDir(), "flag-home")
		t.Setenv("ZBOT_HOME", alt)
		got, err := ResolveHome(flagHome)
		if err != nil {
			t.Fatalf("ResolveHome: %v", err)
		}
		if got != flagHome {
			t.Fatalf("got %s, want %s", got, flagHome)
		}
	})
}

func TestBootstrapFirstRunCreatesStructureOnly(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Tree: config.yaml + five config dirs + data + wards.
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		t.Fatalf("config.yaml missing: %v", err)
	}
	for _, dir := range append(paths.AllConfigDirs(), paths.DataDir, paths.WardsDir) {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s missing or not a dir: %v", dir, err)
		}
	}
	// Structure, never content: no seeded files in the registry dirs.
	for _, dir := range paths.AllConfigDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s seeded with content: %v", dir, entries)
		}
	}
	// The default file parses back as the typed defaults.
	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load after bootstrap: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("bootstrapped config %+v != defaults %+v", cfg, Default())
	}
}

func TestBootstrapNeverOverwrites(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// User edits their config.
	user := "engine: custom\nserver:\n    address: 127.0.0.1:9999\ncompaction:\n    max_context_tokens: 1000\n    reserve_output_tokens: 100\nwards:\n    root: elsewhere\n"
	if err := os.WriteFile(paths.ConfigFile, []byte(user), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	if err := Bootstrap(paths); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	got, _ := os.ReadFile(paths.ConfigFile)
	if string(got) != user {
		t.Fatalf("user config overwritten:\n%s", got)
	}
}

func TestBootstrapIdempotent(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	first, _ := os.ReadFile(paths.ConfigFile)
	if err := Bootstrap(paths); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	second, _ := os.ReadFile(paths.ConfigFile)
	if string(first) != string(second) {
		t.Fatal("second bootstrap changed the config file")
	}
}

func TestLoadStrict(t *testing.T) {
	t.Run("valid config loads", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: w\n")
		cfg, err := Load(paths)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Engine != "eino" || cfg.Server.Address != "127.0.0.1:1" {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("unknown key rejected with key name", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: eino\nsurprise: yes\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "surprise") || !strings.Contains(err.Error(), "config.yaml") {
			t.Fatalf("want error naming key and file, got: %v", err)
		}
	})

	t.Run("invalid yaml rejected with path", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: [unclosed\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "config.yaml") {
			t.Fatalf("want parse error with path, got: %v", err)
		}
	})

	t.Run("absent file rejected", func(t *testing.T) {
		_, paths := tempHome(t)
		if err := Bootstrap(paths); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(paths.ConfigFile); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(paths); err == nil {
			t.Fatal("absent config.yaml must be a hard error")
		}
	})
}

func TestLoadSelfHealsMissingDirs(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(paths); err != nil {
		t.Fatal(err)
	}
	// Delete two directories.
	if err := os.RemoveAll(paths.ConfigDir("skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(paths.DataDir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err != nil {
		t.Fatalf("Load should self-heal: %v", err)
	}
	for _, dir := range []string{paths.ConfigDir("skills"), paths.DataDir} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("self-heal failed for %s: %v", dir, err)
		}
	}
	// And the untouched user file survives with its bytes.
	after, _ := os.ReadFile(paths.ConfigFile)
	if string(before) != string(after) {
		t.Fatal("self-heal modified config.yaml")
	}
}

func TestWardsRootResolution(t *testing.T) {
	_, paths := tempHome(t)
	cfg := Default()
	if got := cfg.WardsRoot(paths); got != filepath.Join(paths.Home, "wards") {
		t.Fatalf("relative root not resolved: %s", got)
	}
	cfg.Wards.Root = "/abs/wards"
	if got := cfg.WardsRoot(paths); got != "/abs/wards" {
		t.Fatalf("absolute root not passed through: %s", got)
	}
}
