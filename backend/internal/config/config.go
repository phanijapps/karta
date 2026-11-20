package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config is the typed config.yaml schema (v1). Loading is strict: unknown
// keys are rejected, and failures never fall back to defaults.
type Config struct {
	Engine     string           `yaml:"engine"`
	Server     ServerConfig     `yaml:"server"`
	Compaction CompactionConfig `yaml:"compaction"`
	Wards      WardsConfig      `yaml:"wards"`
	Providers  []ProviderConfig `yaml:"providers"`
}

// ServerConfig is the daemon's HTTP listen address.
type ServerConfig struct {
	Address string `yaml:"address"`
}

// CompactionConfig carries the token budgets consumed by the compactor.
type CompactionConfig struct {
	MaxContextTokens    int `yaml:"max_context_tokens"`
	ReserveOutputTokens int `yaml:"reserve_output_tokens"`
}

// WardsConfig locates the Code Wards root; relative roots resolve against
// the home.
type WardsConfig struct {
	Root string `yaml:"root"`
}

// ProviderTypeOpenAICompatible is the only supported provider connection
// family today; the string reserves the extension point in config.yaml.
const ProviderTypeOpenAICompatible = "openai-compatible"

// MaxProviders mirrors the contract's ProviderList.providers maxItems —
// loading more than this would produce responses the contract forbids.
const MaxProviders = 64

// envNamePattern is the shape of a credential environment-variable NAME
// (never a value) referenced by api_key_env.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// idPattern mirrors the contract's Id schema so a configured provider
// name can always be represented in API responses.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ProviderConfig is one model provider entry. BaseURL is the provider's
// OpenAI-compatible API root used verbatim (version prefix included, e.g.
// http://localhost:11434/v1); discovery appends only /models. Credentials
// are referenced by environment-variable name, never stored.
type ProviderConfig struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

// WardsRoot resolves the configured wards root: absolute paths pass
// through, relative paths resolve against the home.
func (c Config) WardsRoot(p Paths) string {
	if filepath.IsAbs(c.Wards.Root) {
		return c.Wards.Root
	}
	return filepath.Join(p.Home, c.Wards.Root)
}

// Default returns the documented defaults. Bootstrap marshals these to the
// initial config.yaml, so the default file and this function share one
// source of truth.
func Default() Config {
	return Config{
		Engine:     "eino",
		Server:     ServerConfig{Address: "127.0.0.1:8787"},
		Compaction: CompactionConfig{MaxContextTokens: 128000, ReserveOutputTokens: 8192},
		Wards:      WardsConfig{Root: "wards"},
		Providers:  []ProviderConfig{}, // marshals as `providers: []` so the key is discoverable
	}
}

// Bootstrap creates the control-plane tree, creating missing directories
// (the five config subdirectories, data/, wards/) and — only when absent —
// the default config.yaml. It never overwrites an existing file, seeds no
// content, and is idempotent. Bootstrap is the only writer of config.yaml;
// Load never regenerates it.
func Bootstrap(paths Paths) error {
	if err := ensureDirs(paths); err != nil {
		return err
	}
	raw, err := yaml.Marshal(Default())
	if err != nil {
		return fmt.Errorf("config: marshal defaults: %w", err)
	}
	f, err := os.OpenFile(paths.ConfigFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil // user-owned file — never overwrite
		}
		return fmt.Errorf("config: bootstrap %s: %w", paths.ConfigFile, err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: write %s: %w", paths.ConfigFile, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("config: close %s: %w", paths.ConfigFile, err)
	}
	return nil
}

// ensureDirs self-heals the directory tree without writing any file.
func ensureDirs(paths Paths) error {
	dirs := append(paths.AllConfigDirs(), paths.DataDir, paths.WardsDir, filepath.Dir(paths.ConfigFile))
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("config: ensure %s: %w", dir, err)
		}
	}
	return nil
}

// Load self-heals the directory tree (missing subdirectories are recreated,
// existing files untouched), then strictly loads config.yaml. An absent
// file, invalid YAML, or an unknown key is a hard error naming the file and
// the offending detail — there is no default fallback and no regeneration.
func Load(paths Paths) (Config, error) {
	if err := ensureDirs(paths); err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", paths.ConfigFile, err)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", paths.ConfigFile, err)
	}
	if cfg.Engine == "" {
		return Config{}, fmt.Errorf("config: %s: engine must be set", paths.ConfigFile)
	}
	if err := validateProviders(cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if err := validateLoopback(cfg.Server.Address, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validateLoopback hard-fails a server address whose host is not
// loopback. The daemon is local-first and unauthenticated; the contract's
// loopback-only server entry is a declaration, this is the enforcement.
func validateLoopback(address, file string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("config: %s: server.address %q must be host:port", file, address)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("config: %s: server.address host %q must be loopback (127.0.0.0/8, ::1, or localhost) — the daemon is unauthenticated and local-first", file, host)
	}
	return nil
}

// validateProviders enforces the provider list invariants: non-empty
// unique names, a supported type, an absolute http(s) base URL used
// verbatim, and a well-formed credential env-var name. Errors name the
// file and the offending entry so the user can fix config.yaml directly.
func validateProviders(cfg Config, file string) error {
	if len(cfg.Providers) > MaxProviders {
		return fmt.Errorf("config: %s: providers: more than %d entries", file, MaxProviders)
	}
	seen := make(map[string]struct{}, len(cfg.Providers))
	for i, p := range cfg.Providers {
		at := fmt.Sprintf("%s: providers[%d] name %q", file, i, p.Name)
		if p.Name == "" {
			at = fmt.Sprintf("%s: providers[%d]", file, i)
			return fmt.Errorf("config: %s: name must be set", at)
		}
		if !idPattern.MatchString(p.Name) {
			return fmt.Errorf("config: %s: name must match %s", at, idPattern.String())
		}
		if _, dup := seen[p.Name]; dup {
			return fmt.Errorf("config: %s: duplicate provider name %q", at, p.Name)
		}
		seen[p.Name] = struct{}{}
		if p.Type != ProviderTypeOpenAICompatible {
			return fmt.Errorf("config: %s: unsupported provider type %q (supported: %q)", at, p.Type, ProviderTypeOpenAICompatible)
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("config: %s: base_url must be an absolute http(s) URL (the provider's API root, e.g. http://localhost:11434/v1)", at)
		}
		if p.APIKeyEnv != "" && (!envNamePattern.MatchString(p.APIKeyEnv) || len(p.APIKeyEnv) > 128) {
			return fmt.Errorf("config: %s: api_key_env must be an environment variable name of at most 128 characters (got %q)", at, p.APIKeyEnv)
		}
	}
	return nil
}
