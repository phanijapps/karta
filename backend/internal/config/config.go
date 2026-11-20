package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the typed config.yaml schema (v1). Loading is strict: unknown
// keys are rejected, and failures never fall back to defaults.
type Config struct {
	Engine     string           `yaml:"engine"`
	Server     ServerConfig     `yaml:"server"`
	Compaction CompactionConfig `yaml:"compaction"`
	Wards      WardsConfig      `yaml:"wards"`
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
	return cfg, nil
}
