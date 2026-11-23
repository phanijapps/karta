package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"zbot/backend/internal/registry"
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
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Tree: config.yaml + six config dirs + data (the ward vault is NOT
	// part of the home — it lives outside ~/.zbot; see internal/wards).
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		t.Fatalf("config.yaml missing: %v", err)
	}
	for _, dir := range append(paths.AllConfigDirs(), paths.DataDir) {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s missing or not a dir: %v", dir, err)
		}
	}
	// Structure, never content — the ONE exception is the EXAMPLE
	// roster (seed-operator-roster): agents/ seeds six agent examples
	// and config/skills/ seeds five prompt-craft skills (user-owned
	// after first bootstrap; deleted examples do not resurrect). Every
	// other registry dir stays empty.
	for _, dir := range paths.AllConfigDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", dir, err)
		}
		if dir == paths.ConfigDir("skills") {
			if len(entries) != 5 {
				t.Fatalf("skills seed count: %d", len(entries))
			}
			continue
		}
		if dir == paths.ConfigDir("prompts") {
			if len(entries) != 7 { // role, soul, os-linux/windows/darwin, tools, intent-analysis
				t.Fatalf("prompt seed count: %d", len(entries))
			}
			continue
		}
		if len(entries) != 0 {
			t.Fatalf("%s seeded with content: %v", dir, entries)
		}
	}
	agents, err := os.ReadDir(paths.AgentsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", paths.AgentsDir, err)
	}
	if len(agents) != 6 {
		t.Fatalf("agents seed count: %d", len(agents))
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
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// User edits their config.
	user := "engine: custom\nserver:\n    address: 127.0.0.1:9999\ncompaction:\n    max_context_tokens: 1000\n    reserve_output_tokens: 100\nwards:\n    vault_root: ~/karta-wards\n"
	if err := os.WriteFile(paths.ConfigFile, []byte(user), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	got, _ := os.ReadFile(paths.ConfigFile)
	if string(got) != user {
		t.Fatalf("user config overwritten:\n%s", got)
	}
}

func TestBootstrapIdempotent(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	first, _ := os.ReadFile(paths.ConfigFile)
	if err := Bootstrap(context.Background(), paths); err != nil {
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
		writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\n")
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
		if err := Bootstrap(context.Background(), paths); err != nil {
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
	if err := Bootstrap(context.Background(), paths); err != nil {
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
	for _, dir := range []string{paths.SkillsDir, paths.DataDir} {
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

func TestLoadWardsVaultRoot(t *testing.T) {
	t.Run("vault_root loads verbatim (wards.ResolveVaultRoot resolves it)", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/karta-wards\n")
		cfg, err := Load(paths)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Wards.VaultRoot != "~/karta-wards" {
			t.Fatalf("vault_root not loaded: %q", cfg.Wards.VaultRoot)
		}
	})

	t.Run("deprecated default wards.root is ignored", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: wards\n")
		if _, err := Load(paths); err != nil {
			t.Fatalf("old default root must be ignored, got: %v", err)
		}
	})

	t.Run("customized deprecated wards.root errors with migration path", func(t *testing.T) {
		_, paths := tempHome(t)
		writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    root: elsewhere\n")
		_, err := Load(paths)
		var de *DeprecatedWardsRootError
		if !errors.As(err, &de) {
			t.Fatalf("want typed DeprecatedWardsRootError, got %T: %v", err, err)
		}
		if de.Value != "elsewhere" || !strings.Contains(err.Error(), "wards.vault_root") {
			t.Fatalf("migration error must carry value + path: %+v %v", de, err)
		}
	})
}

func TestLoadProviders(t *testing.T) {
	base := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\n"

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
	base := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\n"

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

func TestServerAddressBinds(t *testing.T) {
	t.Run("loopback and wildcard addresses accepted", func(t *testing.T) {
		for _, addr := range []string{"127.0.0.1:8787", "localhost:8787", "0.0.0.0:8787", "192.168.1.10:8787"} {
			if err := validateBind(addr, "config.yaml"); err != nil {
				t.Fatalf("%s: want accepted, got: %v", addr, err)
			}
		}
	})
	t.Run("malformed and non-IP hosts rejected", func(t *testing.T) {
		for _, addr := range []string{"no-port", "not a host:8787", "http://x:8787"} {
			if err := validateBind(addr, "config.yaml"); err == nil || !strings.Contains(err.Error(), "config.yaml") {
				t.Fatalf("%s: want typed error naming the file, got: %v", addr, err)
			}
		}
	})
}

func TestProvidersDirectoryBootstrapped(t *testing.T) {
	home := t.TempDir()
	paths := NewPaths(filepath.Join(home, "zbot"))
	if err := Bootstrap(context.Background(), paths); err != nil {
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
	writeConfig(t, paths, "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\nproviders:\n    - name: x\n      type: openai-compatible\n      base_url: http://localhost:1/v1\n      api_key: keyring\n")
	_, err := Load(paths)
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("want unknown-key error for api_key in config.yaml, got: %v", err)
	}
}

func TestChittaProviderValidation(t *testing.T) {
	base := func(chitta string) string {
		content := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\n"
		if chitta != "" {
			content += chitta
		}
		return content
	}
	newPaths := func(t *testing.T) Paths {
		t.Helper()
		return NewPaths(filepath.Join(t.TempDir(), "zbot-home"))
	}
	t.Run("explicit local provider loads", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: local\n"))
		if _, err := Load(paths); err != nil {
			t.Fatalf("local provider must load: %v", err)
		}
	})
	t.Run("absent section defaults to local", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base(""))
		if _, err := Load(paths); err != nil {
			t.Fatalf("no chitta section must default: %v", err)
		}
	})
	t.Run("unknown provider fails loudly", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: wat\n"))
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "wat") {
			t.Fatalf("unknown provider must fail naming it: %v", err)
		}
	})
	t.Run("reserved mcps grammar fails until implemented", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: mcps:memory-server\n"))
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "mcps:memory-server") {
			t.Fatalf("reserved mcps provider must fail loudly: %v", err)
		}
	})
}

func TestChittaEmbeddingAdvancedConfig(t *testing.T) {
	base := func(chitta string) string {
		content := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\n"
		if chitta != "" {
			content += chitta
		}
		return content
	}
	newPaths := func(t *testing.T) Paths {
		t.Helper()
		return NewPaths(filepath.Join(t.TempDir(), "zbot-home"))
	}
	t.Run("defaults pin the decided model", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: local\n"))
		cfg, err := Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Chitta.Embedding.Model != DefaultChittaEmbeddingModel || cfg.Chitta.Embedding.Dim != DefaultChittaEmbeddingDim {
			t.Fatalf("defaults: %+v", cfg.Chitta.Embedding)
		}
	})
	t.Run("replacement model with explicit dims is accepted (advanced)", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: local\n    embedding:\n        model: BAAI/bge-base-en-v1.5\n        dim: 768\n"))
		if _, err := Load(paths); err != nil {
			t.Fatalf("replacement model must be configurable: %v", err)
		}
	})
	t.Run("replacement model without explicit dims is rejected", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: local\n    embedding:\n        model: BAAI/bge-base-en-v1.5\n"))
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "dim must be set explicitly") {
			t.Fatalf("swap without dims must fail (no reindex yet): %v", err)
		}
	})
	t.Run("out-of-range dims rejected", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("chitta:\n    provider: local\n    embedding:\n        model: BAAI/bge-base-en-v1.5\n        dim: 9999\n"))
		_, err := Load(paths)
		if err == nil || !strings.Contains(err.Error(), "[2, 4096]") {
			t.Fatalf("dim bounds: %v", err)
		}
	})
}

func TestChittaRecallValidation(t *testing.T) {
	base := func(recall string) string {
		content := "engine: eino\nserver:\n    address: 127.0.0.1:1\ncompaction:\n    max_context_tokens: 10\n    reserve_output_tokens: 2\nwards:\n    vault_root: ~/Documents/wards\nchitta:\n    provider: local\n"
		if recall != "" {
			content += recall
		}
		return content
	}
	newPaths := func(t *testing.T) Paths {
		t.Helper()
		return NewPaths(filepath.Join(t.TempDir(), "zbot-home"))
	}
	t.Run("valid full override loads", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("    recall:\n        weights: {keyword: 0.9, vector: 1.2, graph: 0.5}\n        halflife_days: 14\n        mmr_lambda: 0.4\n        rerank: false\n"))
		if _, err := Load(paths); err != nil {
			t.Fatalf("valid recall config must load: %v", err)
		}
	})
	t.Run("negative weight rejected", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("    recall:\n        weights: {keyword: -1}\n"))
		if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "weights.keyword") {
			t.Fatalf("negative weight must fail: %v", err)
		}
	})
	t.Run("mmr lambda out of range rejected", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("    recall:\n        mmr_lambda: 5\n"))
		if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "mmr_lambda") {
			t.Fatalf("lambda 5 must fail: %v", err)
		}
	})
	t.Run("negative rerank_top rejected", func(t *testing.T) {
		paths := newPaths(t)
		writeConfig(t, paths, base("    recall:\n        rerank_top: -3\n"))
		if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "rerank_top") {
			t.Fatalf("negative rerank_top must fail: %v", err)
		}
	})
}

// Bootstrap seeds the example agents once (user-owned afterward).
func TestBootstrapSeedsExampleAgents(t *testing.T) {
	t.Parallel()
	paths := NewPaths(t.TempDir())
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	dir := paths.AgentsDir
	for _, name := range []string{"research-agent.example.md", "reviewer-agent.example.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing seed %s: %v", name, err)
		}
	}
	// The seeds load as valid agents with delegation metadata.
	reg := registry.New()
	snap, errs := reg.LoadAll(registry.Dirs{Agents: dir, ConfigFile: paths.ConfigFile})
	if len(errs) != 0 {
		t.Fatalf("seed agents do not load: %v", errs)
	}
	if r, ok := snap.Agents["research-agent"]; !ok || r.Description == "" || r.MaxDepth != 0 {
		t.Fatalf("research-agent seed wrong: %+v", r)
	}
	// Idempotent + user-owned: editing a seed survives re-bootstrap.
	owned := filepath.Join(dir, "research-agent.example.md")
	if err := os.WriteFile(owned, []byte("---\nname: research-agent\ndescription: mine now\n---\nBody."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(owned)
	if !strings.Contains(string(raw), "mine now") {
		t.Fatal("bootstrap overwrote a user-owned agent file")
	}
}

// Legacy config/agents migrates one-time to ~/.zbot/agents; user edits
// survive; the legacy dir is removed afterward.
func TestBootstrapMigratesLegacyAgentsDir(t *testing.T) {
	t.Parallel()
	paths := NewPaths(t.TempDir())
	legacy := paths.ConfigDir("agents")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "my-agent.md"), []byte("---\nname: my-agent\ndescription: mine\n---\nPrompt."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.AgentsDir, "my-agent.md")); err != nil {
		t.Fatalf("legacy agent did not migrate: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy dir should be removed after migration")
	}
	// User-owned afterward: edits survive re-bootstrap, no reseeding over.
	owned := filepath.Join(paths.AgentsDir, "my-agent.md")
	if err := os.WriteFile(owned, []byte("---\nname: my-agent\ndescription: edited\n---\nNew."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(owned)
	if !strings.Contains(string(raw), "edited") {
		t.Fatal("re-bootstrap overwrote a migrated user file")
	}
}

// P1 regression: the migration must run for EXISTING installs too (a
// pre-existing config.yaml must not strand a populated legacy config/agents).
func TestBootstrapMigratesLegacyAgentsOnExistingInstall(t *testing.T) {
	t.Parallel()
	paths := NewPaths(t.TempDir())
	// Existing install: bootstrap once (seeds agents at the NEW home)...
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	// ...then simulate a LEGACY install: remove the new home and place
	// files in config/agents (as pre-relocation installs had).
	if err := os.RemoveAll(paths.AgentsDir); err != nil {
		t.Fatal(err)
	}
	legacy := paths.ConfigDir("agents")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "old.md"), []byte("---\nname: old\ndescription: legacy\n---\nL."), 0o644); err != nil {
		t.Fatal(err)
	}
	// config.yaml EXISTS → the old early-return path; migration must still run.
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.AgentsDir, "old.md")); err != nil {
		t.Fatalf("existing-install migration did not run: %v", err)
	}
	// And Load must not resurrect config/agents.
	_, _ = Load(paths)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("Load resurrected the legacy config/agents dir")
	}
}
