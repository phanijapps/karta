package main

// The intent middleware's composition (intent-analyst-agent S1): the
// wrapped engine every root goal run goes through — router → the
// intent-analyst agent's decision (execution row, events, token
// accounting, model-derived limits) → composed prompt + step budget.
// Children/chat/Resume pass through; disabled by config → the plain
// engine. Extracted from main.go (LOC budget).

import (
	"context"
	"encoding/json"
	"log"

	"zbot/backend/chitta"
	"zbot/backend/internal/config"
	"zbot/backend/internal/domain"
	einoengine "zbot/backend/internal/engine/eino"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/intent"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/resourceindex"
	zbotstore "zbot/backend/internal/store"
	"zbot/backend/internal/wards"
)

// wireIntentMiddleware returns the engine goal runs use.
type intentDeps struct {
	cfg            config.Config
	reg            *registry.Registry
	db             *zbotstore.Store
	bus            *eventbus.Bus
	eng            *einoengine.Adapter
	provStore      *providers.Store
	catalog        *providers.Catalog
	wardVault      *wards.Vault
	chitta         *chitta.System
	resIndex       *resourceindex.Indexer
	composePersona func() (string, error)
	configDir      string
	ctx            context.Context
}

// wireIntentMiddleware returns the engine goal runs use: ALWAYS the
// wrapper (a disabled kill-switch still emits analysis_bypassed and
// keeps the ward orchestrator model — analysis is the only thing it
// turns off).
func wireIntentMiddleware(d intentDeps) domain.AgentEngine {
	cfg, reg, db, bus, eng, provStore, catalog, wardVault, composePersona, configDir, ctx :=
		d.cfg, d.reg, d.db, d.bus, d.eng, d.provStore, d.catalog, d.wardVault, d.composePersona, d.configDir, d.ctx
	runEngine := domain.AgentEngine(&intent.Wrapper{
		Engine: eng,
		Analyzer: &intent.Analyzer{
			Completer: llmCaller{provStore, providers.NewChatClient()},
			Snapshot:  reg.Snapshot,
		},
		Snapshot: reg.Snapshot,
		Wards: func() []intent.WardInfo {
			infos, err := wardVault.Index(ctx)
			if err != nil {
				return nil
			}
			out := make([]intent.WardInfo, len(infos))
			for i, wi := range infos {
				out[i] = intent.WardInfo{Name: wi.Name, Scope: wi.CanonicalTitle}
			}
			return out
		},
		RecallFn: func(ctx context.Context, goal string) ([]string, []string) {
			// Memory: fused recall minus resource-index records.
			page, err := d.chitta.Recall(ctx, domain.RecallRequest{Query: goal, Limit: 8}, domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
			if err != nil {
				return nil, nil
			}
			var memory []string
			for _, h := range page.Items {
				if h.Record.Kind.IsResourceKind() {
					continue
				}
				memory = append(memory, h.Record.Content)
			}
			// Knowledge: the same fused recall's GRAPH-lane hits —
			// both chitta domains in one query, attributed by lane
			// (graph hits are knowledge-graph contributions).
			var knowledge []string
			for _, h := range page.Items {
				if h.Record.Kind.IsResourceKind() {
					continue
				}
				for _, lane := range h.Lanes {
					if lane == domain.LaneGraph {
						knowledge = append(knowledge, h.Record.Content)
						break
					}
				}
			}
			// Graph-lane hits double as memory: drop them there.
			kset := map[string]bool{}
			for _, k := range knowledge {
				kset[k] = true
			}
			memOnly := memory[:0]
			for _, m := range memory {
				if !kset[m] {
					memOnly = append(memOnly, m)
				}
			}
			return memOnly, knowledge
		},
		Cfg: intent.Config{
			Enabled: cfg.Intent.On(),
			// Per-ward orchestrator: a bound ward's model override
			// runs the hosted session — and, when a ward orchestrates,
			// wins the analysis model (operator decision).
			OrchestratorModel: wardOrchestratorModel(wardVault, db),
			// Auto-title untitled research sessions from the first
			// analysis (never overwrites a user title).
			TitleSession: func(sessionID, title string) {
				if _, err := db.SetSessionTitleIfUntitled(sessionID, title); err != nil {
					log.Printf("zbotd: session %s auto-title failed: %v", sessionID, err)
				}
			},
			// Model precedence (after the ward): the Settings card's
			// intent-analysis.json, the agent spec, config.yaml, the run.
			Settings:       func() intent.Settings { return intent.ReadSettings(configDir) },
			AnalysisModel:  cfg.Intent.Model,
			ComposePersona: composePersona,
			// The model's own limits, through the one resolution chain
			// (catalog window → limits table → 32k).
			Window: analysisWindow(provStore, catalog),
			// The intent-analyst's dedicated execution row: opened before
			// the decision, closed with its status + token usage.
			BeginAnalysis: func(sessionID, goal, model string) string {
				id, err := db.CreateAnalysisExecution(sessionID, registry.SystemAgentIntentAnalyst, goal)
				if err != nil {
					log.Printf("zbotd: analysis execution row for %s failed: %v", sessionID, err)
					return ""
				}
				return id
			},
			FinishAnalysis: func(execID, status, errMsg string, tokensIn, tokensOut int64) {
				if err := db.FinishExecution(execID, status, errMsg, tokensIn, tokensOut); err != nil {
					log.Printf("zbotd: analysis execution %s finish failed: %v", execID, err)
				}
			},
			// Attempted-durable-first, the engine's own posture: the
			// stored payload carries text AND the typed analysis (the
			// SSE replay reads both).
			Emit: func(ev domain.TimelineEvent) {
				payload, _ := json.Marshal(map[string]any{
					"text":            ev.Text,
					"intent_analysis": ev.IntentAnalysis,
				})
				_ = db.AppendEvent(zbotstore.Event{ //nolint:errcheck // attempted-durable-first (F-15 posture)
					ID: ev.ID, SessionID: ev.SessionID, Kind: string(ev.Kind), Payload: string(payload),
				})
				_ = bus.Publish(context.WithoutCancel(ctx), ev) //nolint:errcheck // fire-and-forget emit (F-15)
			},
			RecallResources: func(ctx context.Context, goal string) intent.Resources {
				hits, err := d.resIndex.Relevant(ctx, goal, 12)
				if err != nil {
					return intent.Resources{} // listing fallback inside the wrapper
				}
				res := intent.Resources{}
				for _, h := range hits {
					switch h.Kind {
					case resourceindex.KindSkill:
						res.Skills = append(res.Skills, h.Line())
					case resourceindex.KindAgent:
						res.Agents = append(res.Agents, h.Line())
					case resourceindex.KindMCP:
						res.MCPs = append(res.MCPs, h.Line())
					}
				}
				return res
			},
			KnownMCPs: func(name string) bool {
				known, err := d.resIndex.KnownMCP(ctx, name)
				return err == nil && known
			},
		},
	},
	)
	return runEngine
}
