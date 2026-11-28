package config

// The documented defaults (Bootstrap marshals them into the initial
// config.yaml) and the knobs whose default IS a decision: the delegation
// depth hold and the engine run path.

// DelegationConfig bounds the delegation tree. MaxDepth is the operator's
// hold on nesting: 1 (the default) means the ROOT may delegate and
// children never — whatever any agent's max_depth grants — until the
// operator raises it (delegation-truthfulness, decision 2).
type DelegationConfig struct {
	MaxDepth int `yaml:"max_depth"`
}

// Engine run paths (engine-adk-spine). The hand-rolled ReAct loop
// (legacy) and the Eino ADK spine (adk) coexist behind domain.AgentEngine
// for one soak cycle; both pass the conformance suite. The default is adk
// since the delegation slice (engine-adk-spine); legacy stays selectable.
const (
	LoopLegacy = "legacy"
	LoopADK    = "adk"
)

// Default returns the documented defaults. Bootstrap marshals these to the
// initial config.yaml, so the default file and this function share one
// source of truth.
func Default() Config {
	return Config{
		Engine:       "eino",
		EngineLoop:   LoopADK,
		Delegation:   DelegationConfig{MaxDepth: DefaultDelegationMaxDepth},
		Server:       ServerConfig{Address: "127.0.0.1:8787"},
		Compaction:   CompactionConfig{MaxContextTokens: 128000, ReserveOutputTokens: 8192, ToolResultCap: DefaultToolResultCap},
		Skills:       SkillsConfig{Injection: SkillsProgressive},
		Wards:        WardsConfig{VaultRoot: ""}, // empty = OS-aware ~/Documents/wards (wards.ResolveVaultRoot)
		SystemPrompt: SystemPromptConfig{Role: "role", Soul: "soul", OS: "auto", Tools: "tools", Custom: []string{"research-protocol"}},
		Intent:       IntentConfig{Enabled: boolPtrLocal(true)},
		Providers:    []ProviderConfig{}, // marshals as `providers: []` so the key is discoverable
		Chitta: ChittaConfig{
			Provider:  "local",
			Embedding: ChittaEmbeddingConfig{Model: DefaultChittaEmbeddingModel, Dim: DefaultChittaEmbeddingDim},
			Recall:    ChittaRecallConfig{Weights: RecallWeights{Keyword: 1.0, Vector: 1.0, Graph: 0.8}, HalflifeDays: 30, BlendRecency: 0.3, BlendConfidence: 0.15, BlendMentions: 0.1, MMRLambda: 0.7, Rerank: boolPtrLocal(true), RerankTop: 50},
		},
	}
}
