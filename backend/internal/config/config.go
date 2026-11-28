package config

import (
	"bytes"
	"context"
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
	Engine       string             `yaml:"engine"`
	EngineLoop   string             `yaml:"engine_loop"`
	Server       ServerConfig       `yaml:"server"`
	Compaction   CompactionConfig   `yaml:"compaction"`
	Wards        WardsConfig        `yaml:"wards"`
	Providers    []ProviderConfig   `yaml:"providers"`
	Chitta       ChittaConfig       `yaml:"chitta"`
	SystemPrompt SystemPromptConfig `yaml:"system_prompt"`
	Intent       IntentConfig       `yaml:"intent_analysis"`
	Delegation   DelegationConfig   `yaml:"delegation"`
	Skills       SkillsConfig       `yaml:"skills"`
}

// SystemPromptConfig selects the composed system prompt's static
// sections from the prompt registry (config/prompts/<name>.md). Empty
// optional names skip the section; `os` accepts "auto" (pick the
// os-<platform> pack by the daemon's OS), an explicit
// linux|windows|darwin, or "none". An install whose config.yaml
// predates this key behaves as the defaults (Load back-fills).
type SystemPromptConfig struct {
	Role   string   `yaml:"role"`
	Soul   string   `yaml:"soul,omitempty"`
	OS     string   `yaml:"os,omitempty"`
	Tools  string   `yaml:"tools,omitempty"`
	Custom []string `yaml:"custom,omitempty"`
}

// DefaultDelegationMaxDepth is the shipped hold on nesting: the root
// delegates, children do not.
const DefaultDelegationMaxDepth = 1

// IntentConfig gates the pre-execution intent middleware for goal
// runs (chat is never analyzed). Enabled is a POINTER: an absent key
// (config files predating this change) means ON — the default — while
// an explicit `enabled: false` is the kill-switch. A plain bool would
// silently disable analysis on every existing install.
type IntentConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty"`
	Model   string `yaml:"model,omitempty"` // optional qualified provider/model for the analysis call; empty = the run's model
}

// On reports whether intent analysis is enabled (absent key = on).
func (c IntentConfig) On() bool { return c.Enabled == nil || *c.Enabled }

// ServerConfig is the daemon's HTTP listen address.
type ServerConfig struct {
	Address string `yaml:"address"`
	// ApiToken optionally guards the OpenAI-compatible /v1 channel
	// (bearer). Empty (default) keeps the loopback-only posture.
	ApiToken string `yaml:"api_token"`
}

// ChittaConfig selects the memory/knowledge provider (design D7 of the
// add-chitta change). "local" is the built-in LadybugDB provider at
// <data-dir>/chitta/; the "mcps:<name>" grammar is reserved for the
// future MCP-backed provider and fails loudly until it exists.
type ChittaConfig struct {
	Provider  string                `yaml:"provider"`
	Embedding ChittaEmbeddingConfig `yaml:"embedding"`
	Recall    ChittaRecallConfig    `yaml:"recall"`
}

// ChittaRecallConfig carries the pipeline tuning knobs; zeros mean the
// calibrated defaults (design D4/D5 of add-chitta-recall-quality).
type ChittaRecallConfig struct {
	Weights         RecallWeights `yaml:"weights"`
	HalflifeDays    float64       `yaml:"halflife_days,omitempty"`
	BlendRecency    float64       `yaml:"blend_recency,omitempty"`
	BlendConfidence float64       `yaml:"blend_confidence,omitempty"`
	BlendMentions   float64       `yaml:"blend_mentions,omitempty"`
	MMRLambda       float64       `yaml:"mmr_lambda,omitempty"`
	Rerank          *bool         `yaml:"rerank,omitempty"`
	RerankTop       int           `yaml:"rerank_top,omitempty"`
}

// RecallWeights are the RRF lane weights.
type RecallWeights struct {
	Keyword float64 `yaml:"keyword,omitempty"`
	Vector  float64 `yaml:"vector,omitempty"`
	Graph   float64 `yaml:"graph,omitempty"`
}

// ChittaEmbeddingConfig is the ADVANCED embedding contract: config-file
// only — NOT exposed as an editable setting anywhere until reindex
// support exists (swapping model/dim requires a database created at that
// dimension; mismatched opens fail loudly, never a silent re-index).
// Defaults pin the decided model (fastembed-go + BAAI/bge-small-en-v1.5,
// 384-dim); any replacement model with its own dimension may be
// configured here.
type ChittaEmbeddingConfig struct {
	Model string `yaml:"model"`
	Dim   int    `yaml:"dim"`
}

// Default embedding model constants (add-chitta design.md).
const (
	DefaultChittaEmbeddingModel = "BAAI/bge-small-en-v1.5"
	DefaultChittaEmbeddingDim   = 384
)

// CompactionConfig carries the token budgets consumed by the compactor.
type CompactionConfig struct {
	MaxContextTokens    int `yaml:"max_context_tokens"`
	ReserveOutputTokens int `yaml:"reserve_output_tokens"`
	// Summarizer and ToolResultCap: the in-run context budget
	// (context_budget.go).
	Summarizer    string `yaml:"summarizer,omitempty"`
	ToolResultCap int    `yaml:"tool_result_cap,omitempty"`
}

// WardsConfig locates the ward vault. The vault is USER DATA and lives
// outside ~/.zbot by design (see internal/wards and the constitution's
// scoped vault exception): empty vault_root means the OS-aware default
// ~/Documents/wards, resolved by wards.ResolveVaultRoot (which also
// validates: ~ expansion, absolute paths only). Root is the deprecated
// Code Wards key — decoded so existing config.yaml files keep loading,
// but a customized value is a hard Load error pointing at vault_root
// (the placeholder feature never shipped; the old default "wards" is
// ignored silently).
type WardsConfig struct {
	VaultRoot string `yaml:"vault_root"`
	Root      string `yaml:"root,omitempty"` // deprecated Code Wards key
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

// Bootstrap creates the control-plane tree (config subdirectories, data/), migrates a legacy config/agents directory to ~/.zbot/agents
// (one-time, non-destructive), writes the defaults when config.yaml is
// absent, and seeds the example roster ONCE per home (a marker file
// gates it: fresh homes AND pre-marker upgrades gain their missing
// examples; after the marker, deletions stay deleted). It never
// overwrites an existing file and is idempotent. Bootstrap is the only
// writer of config.yaml; Load never regenerates it.
func Bootstrap(ctx context.Context, paths Paths) error {
	// Legacy migrations run BEFORE ensureDirs (their guards probe the
	// destination's absence; ensureDirs would create it) and for
	// EXISTING installs too (the config.yaml early-return below must
	// not strand a populated legacy tree).
	if _, err := migrateLegacyAgents(paths); err != nil {
		return err
	}
	if err := migrateLegacySkillsHooks(paths); err != nil {
		return err
	}
	if err := ensureDirs(paths); err != nil {
		return err
	}
	raw, err := yaml.Marshal(Default())
	if err != nil {
		return fmt.Errorf("config: marshal defaults: %w", err)
	}
	f, err := os.OpenFile(paths.ConfigFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("config: bootstrap %s: %w", paths.ConfigFile, err)
		}
		// User-owned config.yaml — fall through: an EXISTING home still
		// receives the example roster once (the marker decides).
	} else {
		if _, err := f.Write(raw); err != nil {
			_ = f.Close()
			return fmt.Errorf("config: write %s: %w", paths.ConfigFile, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("config: close %s: %w", paths.ConfigFile, err)
		}
	}
	return seedRoster(ctx, paths)
}

// exampleRosterMarker gates the one-time example-roster seeding: absent
// → seed (fresh homes and pre-marker upgrades alike); present → never
// again, so user deletions after the first seeding do not resurrect.
const exampleRosterMarker = ".example-roster-seeded"

// seedRoster runs the write-once example seeding once per home, then
// drops the marker. Every publish is atomic (link-based), so concurrent
// bootstraps cannot observe partial seeds: whichever writer finishes a
// file first publishes it complete, the other's link is a no-op, and
// the marker lands only after a full successful walk. A mid-walk
// failure leaves no marker, so the next bootstrap retries.
func seedRoster(ctx context.Context, paths Paths) error {
	return seedExamples(ctx, paths)
}

// migrateLegacyAgents one-time-moves files from the legacy config/agents
// to ~/.zbot/agents (only when the new directory is absent). Reports
// whether a migration happened.
func migrateLegacyAgents(paths Paths) (bool, error) {
	dir := paths.AgentsDir
	legacy := paths.ConfigDir("agents")
	if dir == "" || legacy == "" || legacy == dir {
		return false, nil
	}
	entries, err := os.ReadDir(legacy)
	if err != nil || len(entries) == 0 {
		return false, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return false, nil // new home exists: nothing to migrate
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("config: probe agents dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("config: migrate agents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Rename(filepath.Join(legacy, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return false, fmt.Errorf("config: migrate agent %s: %w", e.Name(), err)
		}
	}
	_ = os.Remove(legacy)
	return true, nil
}

// ensureDirs self-heals the directory tree without writing any file.
func ensureDirs(paths Paths) error {
	dirs := append(paths.AllConfigDirs(), paths.DataDir, paths.AgentsDir, paths.SkillsDir, paths.HooksDir, filepath.Dir(paths.ConfigFile))
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
	if cfg.EngineLoop == "" {
		cfg.EngineLoop = LoopADK // absent key: the spine (slice 2 flipped the default; legacy stays selectable for the soak cycle)
	} else if cfg.EngineLoop != LoopLegacy && cfg.EngineLoop != LoopADK {
		return Config{}, fmt.Errorf("config: %s: engine_loop must be one of %q, %q (got %q)", paths.ConfigFile, LoopADK, LoopLegacy, cfg.EngineLoop)
	}
	if err := applyContextBudgetDefaults(&cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if cfg.Delegation.MaxDepth == 0 {
		cfg.Delegation.MaxDepth = DefaultDelegationMaxDepth // absent key: the hold
	} else if cfg.Delegation.MaxDepth < 0 {
		return Config{}, fmt.Errorf("config: %s: delegation.max_depth must be at least 1", paths.ConfigFile)
	}
	if err := validateProviders(cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if err := validateChitta(&cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if err := validateBind(cfg.Server.Address, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if err := validateWards(cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	backfillSystemPrompt(&cfg)
	if err := validateSystemPrompt(cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// backfillSystemPrompt restores the defaults for config.yaml files that
// predate the system_prompt key (strict decode yields the zero value).
// A configured block replaces the defaults whole: empty optional names
// skip sections by design.
func backfillSystemPrompt(cfg *Config) {
	sp := cfg.SystemPrompt
	if sp.Role == "" && sp.Soul == "" && sp.OS == "" && sp.Tools == "" && len(sp.Custom) == 0 {
		cfg.SystemPrompt = Default().SystemPrompt
	}
}

// validateSystemPrompt enforces the composed prompt's config surface:
// a role name and a valid os mode.
func validateSystemPrompt(cfg Config, file string) error {
	if cfg.SystemPrompt.Role == "" {
		return fmt.Errorf("config: %s: system_prompt.role must name a prompt section", file)
	}
	switch cfg.SystemPrompt.OS {
	case "", "auto", "none", "linux", "windows", "darwin":
	default:
		return fmt.Errorf("config: %s: system_prompt.os must be auto, none, linux, windows, or darwin (got %q)", file, cfg.SystemPrompt.OS)
	}
	return nil
}

// DeprecatedWardsRootError is the typed migration error for a
// customized `wards.root` value: the Code Wards placeholder never
// shipped, so the key is unused — the operator must move the intent to
// `wards.vault_root` (absolute path; default ~/Documents/wards).
type DeprecatedWardsRootError struct {
	Value string
}

func (e *DeprecatedWardsRootError) Error() string {
	return fmt.Sprintf("config: wards.root %q is deprecated and unused (the Code Wards placeholder never shipped); set wards.vault_root (absolute path, default ~/Documents/wards) instead", e.Value)
}

// validateWards enforces the wards config surface: vault_root itself is
// resolved and validated by wards.ResolveVaultRoot (daemon wiring), but
// a customized deprecated wards.root fails loudly with the migration
// path — silently ignoring a configured value would hide user intent.
func validateWards(cfg Config, file string) error {
	if cfg.Wards.Root != "" && cfg.Wards.Root != "wards" {
		return fmt.Errorf("%s: %w", file, &DeprecatedWardsRootError{Value: cfg.Wards.Root})
	}
	return nil
}

// validateChitta enforces the provider selection: unknown or
// unimplemented providers fail loudly, never a silent fallback (spec
// chitta "Provider selection via configuration").
func validateChitta(cfg *Config, file string) error {
	provider := cfg.Chitta.Provider
	if provider == "" {
		provider = "local"
		cfg.Chitta.Provider = provider
	}
	if provider != "local" {
		return fmt.Errorf("config: %s: chitta.provider %q is not known to the daemon (available: local; mcps:<name> is reserved for the MCP-backed provider)", file, provider)
	}
	// Embedding contract (advanced, config-file only). A replacement
	// model with a different dimension MAY be configured, but its dim
	// must be explicit — reindex is not yet supported, so a database
	// created at another dimension fails loudly at open.
	if cfg.Chitta.Embedding.Model == "" {
		cfg.Chitta.Embedding.Model = DefaultChittaEmbeddingModel
	} else if cfg.Chitta.Embedding.Model != DefaultChittaEmbeddingModel && cfg.Chitta.Embedding.Dim == 0 {
		return fmt.Errorf("config: %s: chitta.embedding.dim must be set explicitly when embedding.model is not the default %q (reindex is not yet supported)", file, DefaultChittaEmbeddingModel)
	}
	if cfg.Chitta.Embedding.Dim != 0 && (cfg.Chitta.Embedding.Dim < 2 || cfg.Chitta.Embedding.Dim > 4096) {
		return fmt.Errorf("config: %s: chitta.embedding.dim must be within [2, 4096], got %d", file, cfg.Chitta.Embedding.Dim)
	}
	r := cfg.Chitta.Recall
	for name, v := range map[string]float64{
		"weights.keyword": r.Weights.Keyword, "weights.vector": r.Weights.Vector, "weights.graph": r.Weights.Graph,
		"blend_recency": r.BlendRecency, "blend_confidence": r.BlendConfidence, "blend_mentions": r.BlendMentions,
	} {
		if v < 0 {
			return fmt.Errorf("config: %s: chitta.recall.%s must be >= 0, got %v", file, name, v)
		}
	}
	if r.MMRLambda < 0 || r.MMRLambda > 1 {
		return fmt.Errorf("config: %s: chitta.recall.mmr_lambda must be within [0, 1], got %v", file, r.MMRLambda)
	}
	if r.HalflifeDays < 0 {
		return fmt.Errorf("config: %s: chitta.recall.halflife_days must be >= 0, got %v", file, r.HalflifeDays)
	}
	if r.RerankTop < 0 || r.RerankTop > 100 {
		return fmt.Errorf("config: %s: chitta.recall.rerank_top must be within [0, 100], got %d", file, r.RerankTop)
	}
	// Normalize the default dimension last so the loaded config always
	// carries the effective model/dimension (the composition root reads
	// these).
	if cfg.Chitta.Embedding.Dim == 0 {
		cfg.Chitta.Embedding.Dim = DefaultChittaEmbeddingDim
	}
	return nil
}

// validateBind enforces host:port. The daemon is unauthenticated and
// local-first: loopback is the default and recommended bind. Exposing a
// wildcard (0.0.0.0/::) or LAN address is an explicit operator choice —
// accepted here — that puts an unauthenticated API (authored env values,
// the MCP command probe) on the network; the request guard adapts (see
// internal/server/guard.go) and the operator owns that trust decision.
func validateBind(address, file string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("config: %s: server.address %q must be host:port", file, address)
	}
	if host == "localhost" || host == "" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("config: %s: server.address host %q must be an IP address, a wildcard, or localhost", file, host)
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

func boolPtrLocal(b bool) *bool { return &b }
