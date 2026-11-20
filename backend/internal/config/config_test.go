package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	if !reflect.DeepEqual(cfg, Default()) {
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

func TestLoadProviders(t *testing.T) {
	base := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: w\n"

	t.Run("absent providers key loads as empty list", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base)
		cfg, err := Load(paths)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.Providers) != 0 {
			t.Fatalf("want empty providers, got %+v", cfg.Providers)
		}
	})

	t.Run("valid provider loads with fields intact", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: ollama\n      type: openai-compatible\n      base_url: http://localhost:11434/v1\n    - name: zai\n      type: openai-compatible\n      base_url: https://api.z.ai/v1\n      api_key_env: ZAI_API_KEY\n")
		cfg, err := Load(paths)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.Providers) != 2 {
			t.Fatalf("want 2 providers, got %+v", cfg.Providers)
		}
		got := cfg.Providers[0]
		if got.Name != "ollama" || got.Type != "openai-compatible" || got.BaseURL != "http://localhost:11434/v1" || got.APIKeyEnv != "" {
			t.Fatalf("unexpected provider: %+v", got)
		}
		if cfg.Providers[1].APIKeyEnv != "ZAI_API_KEY" {
			t.Fatalf("unexpected credential ref: %+v", cfg.Providers[1])
		}
	})

	t.Run("base url used verbatim without version prefix", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: ollama\n      type: openai-compatible\n      base_url: http://localhost:11434\n")
		cfg, err := Load(paths)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers[0].BaseURL != "http://localhost:11434" {
			t.Fatalf("base_url must round-trip verbatim, got %q", cfg.Providers[0].BaseURL)
		}
	})

	t.Run("duplicate names rejected naming entry", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: ollama\n      type: openai-compatible\n      base_url: http://localhost:11434/v1\n    - name: ollama\n      type: openai-compatible\n      base_url: http://other:1/v1\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "duplicate provider name") || !strings.Contains(err.Error(), "ollama") || !strings.Contains(err.Error(), "config.yaml") {
			t.Fatalf("want duplicate-name error naming file and entry, got: %v", err)
		}
	})

	t.Run("unsupported type rejected naming entry", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: x\n      type: anthropic\n      base_url: https://api.example/v1\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "unsupported provider type") || !strings.Contains(err.Error(), "anthropic") {
			t.Fatalf("want unsupported-type error, got: %v", err)
		}
	})

	t.Run("invalid base url rejected", func(t *testing.T) {
		for _, bad := range []string{"", "localhost:11434/v1", "ftp://localhost:11434/v1", "/v1"} {
			_, paths := tempHome(t)
			writeConfig(t, paths, base+"providers:\n    - name: x\n      type: openai-compatible\n      base_url: "+strconv.Quote(bad)+"\n")
			_, err := Load(paths)
			if err == nil || !strings.Contains(err.Error(), "base_url") {
				t.Fatalf("base_url %q: want error, got: %v", bad, err)
			}
		}
	})

	t.Run("malformed api_key_env rejected", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: x\n      type: openai-compatible\n      base_url: http://localhost:1/v1\n      api_key_env: \"$SECRET\"\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "api_key_env") {
			t.Fatalf("want api_key_env error, got: %v", err)
		}
	})
}

func TestLoadProviderBoundaries(t *testing.T) {
	base := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: w\n"

	t.Run("name outside contract Id pattern rejected", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, base+"providers:\n    - name: my provider\n      type: openai-compatible\n      base_url: http://localhost:1/v1\n")
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "name must match") {
			t.Fatalf("want Id-pattern error, got: %v", err)
		}
	})

	t.Run("more than 64 providers rejected", func(t *testing.T) {
		_, paths := tempHome(t)
		yaml := base + "providers:\n"
		for i := 0; i < 65; i++ {
			yaml += fmt.Sprintf("    - name: p%d\n      type: openai-compatible\n      base_url: http://localhost:1/v1\n", i)
		}
		writeConfig(t, paths, yaml)
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "more than 64") {
			t.Fatalf("want provider-count error, got: %v", err)
		}
	})
}

func TestServerAddressLoopbackOnly(t *testing.T) {
	base := func(addr string) string {
		return "engine: eino\nserver:\n    address: " + strconv.Quote(addr) + "\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: w\n"
	}
	t.Run("loopback addresses accepted", func(t *testing.T) {
		for _, addr := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787", "127.42.0.9:9000"} {
			_, paths := tempHome(t)
			writeConfig(t, paths, base(addr))
			if _, err := Load(paths); err != nil {
				t.Fatalf("%s: %v", addr, err)
			}
		}
	})
	t.Run("non-loopback addresses rejected", func(t *testing.T) {
		for _, addr := range []string{"0.0.0.0:8787", "192.168.1.5:8787", "example.com:8787", "8787"} {
			_, paths := tempHome(t)
			writeConfig(t, paths, base(addr))
			_, err := Load(paths)
			if err == nil || !strings.Contains(err.Error(), "loopback") || !strings.Contains(err.Error(), "config.yaml") {
				t.Fatalf("%s: want loopback error naming file, got: %v", addr, err)
			}
		}
	})
}

func TestProvidersDirectoryBootstrapped(t *testing.T) {
	home := t.TempDir()
	paths := NewPaths(filepath.Join(home, "zbot"))
	if err := Bootstrap(paths); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(paths.ProvidersDir); err != nil || !info.IsDir() {
		t.Fatalf("config/providers missing after bootstrap: %v", err)
	}
	// Self-heal recreates it when removed.
	if err := os.RemoveAll(paths.ProvidersDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(paths.ProvidersDir); err != nil || !info.IsDir() {
		t.Fatalf("config/providers not self-healed: %v", err)
	}
}

func TestConfigYAMLRejectsKeyringMarker(t *testing.T) {
	// config.yaml provider entries are environment-reference only; the
	// api_key marker is rejected by strict decoding (unknown key).
	_, paths := tempHome(t)
	writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: w\nproviders:\n    - name: x\n      type: openai-compatible\n      base_url: http://localhost:1/v1\n      api_key: keyring\n")
	_, err := Load(paths)
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("want unknown-key error for api_key in config.yaml, got: %v", err)
	}
}
