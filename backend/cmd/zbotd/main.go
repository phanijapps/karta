// Command zbotd is the local-first Karta daemon: it resolves the control
// plane, strictly loads config.yaml, and serves the contract surface on
// the configured loopback address. Wiring only — behavior lives in
// internal/*.
package main

import (
	"path/filepath"

	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"zbot/backend/chitta"
	"zbot/backend/distiller"
	"zbot/backend/internal/domain"

	einoengine "zbot/backend/internal/engine/eino"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/config"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/scheduler"
	"zbot/backend/internal/server"
	"zbot/backend/internal/skillhub"
	zbotstore "zbot/backend/internal/store"
	"zbot/backend/internal/wards"

	"zbot/backend/internal/intent"
	"zbot/backend/internal/resourceindex"
	"zbot/backend/internal/sysprompt"
	"zbot/backend/internal/tools"
)

// version is stamped via -ldflags "-X main.version=..." in releases.
var version = "dev"

func main() {
	homeFlag := flag.String("home", "", "z-Bot home root (default ~/.zbot; ZBOT_HOME also honored)")
	flag.Parse()

	// The one context root (go-rules 2.2: Background only in main). It
	// is created before any wiring so bootstrap-era work is cancellable.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *homeFlag); err != nil {
		log.Fatalf("zbotd: %v", err)
	}
}

func run(ctx context.Context, homeFlag string) error {
	home, err := config.ResolveHome(homeFlag)
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	paths := config.NewPaths(home)
	if err := config.Bootstrap(ctx, paths); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err // Load errors already name config.yaml and the detail.
	}

	// The provider store merges config.yaml entries with the managed
	// config/providers/ directory and watches it for external changes.
	// Close is deferred IMMEDIATELY: every later early-return (vault
	// bootstrap, engine wiring, listen) must still stop the watcher.
	provStore, err := providers.NewStore(paths, cfg, providers.NewKeyringStore(home))
	if err != nil {
		return fmt.Errorf("providers: %w", err)
	}
	defer provStore.Close() // stops the providers watcher on any exit path

	// Skill marketplace plane: pinned skills.sh index, GitHub fetcher,
	// staged-import capabilities (startup reclaims orphaned stages).
	skillStore, err := skillhub.NewStore(paths.DataDir + "/../tmp/skill-import")
	if err != nil {
		return fmt.Errorf("skill staging: %w", err)
	}

	// Control-plane registry: load prompts/agents/skills/hooks/mcps once,
	// then hot-reload on every change under the config tree. MCP and
	// skill management (Settings) mutate through the registry's write
	// paths; the watcher coexists with them by design.
	reg := registry.New()
	if _, errs := reg.LoadAll(registry.Dirs{
		Prompts:    paths.ConfigDir("prompts"),
		Agents:     paths.AgentsDir,
		Skills:     paths.SkillsDir,
		Hooks:      paths.HooksDir,
		MCPs:       paths.ConfigDir("mcps"),
		Schedules:  paths.ConfigDir("schedules"),
		ConfigFile: paths.ConfigFile,
	}); len(errs) > 0 {
		for _, e := range errs {
			log.Printf("zbotd: registry: %v", e)
		}
	}

	db, err := zbotstore.Open(paths.DataDir + "/zbot.db")
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer func() { _ = db.Close() }()

	bus := eventbus.New()

	// Chitta (memory + knowledge core): opened per the `chitta:` config
	// section — the composition root wiring (review round 1). Consumers
	// (distillation, briefing middleware) attach in later changes.
	chittaSys, err := chitta.Open(filepath.Join(paths.DataDir, "chitta"), chitta.Options{
		Bus:          bus,
		EmbeddingDim: cfg.Chitta.Embedding.Dim,
	})
	if err != nil {
		return fmt.Errorf("chitta: %w", err)
	}
	defer func() { _ = chittaSys.Close() }()
	// Agent tool plane: builtin tools + the seven chitta tools
	// (memory/knowledge), merged — spec add-chitta-engine-tools.
	chittaTools := chittaSys.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	allTools := tools.Multi(tools.BuiltinProvider{}, chittaTools)

	rerank := true
	if cfg.Chitta.Recall.Rerank != nil {
		rerank = *cfg.Chitta.Recall.Rerank
	}
	chittaSys.ConfigureRecall(domain.RecallSettings{
		WeightKeyword:       cfg.Chitta.Recall.Weights.Keyword,
		WeightVector:        cfg.Chitta.Recall.Weights.Vector,
		WeightGraph:         cfg.Chitta.Recall.Weights.Graph,
		BlendRecency:        cfg.Chitta.Recall.BlendRecency,
		BlendConfidence:     cfg.Chitta.Recall.BlendConfidence,
		BlendMentions:       cfg.Chitta.Recall.BlendMentions,
		RecencyHalfLifeDays: cfg.Chitta.Recall.HalflifeDays,
		MMRLambda:           cfg.Chitta.Recall.MMRLambda,
		Rerank:              rerank,
		RerankTop:           cfg.Chitta.Recall.RerankTop,
	})
	// Embedding producer: attach ONLY when the setup-warmed model cache
	// is present — a cold cache would download at runtime (local-first
	// violation); keyword+graph recall covers the gap.
	if chitta.EmbedModelCacheWarm(chitta.DefaultEmbedModelDir()) {
		embedder, err := chitta.NewFastEmbedder(cfg.Chitta.Embedding.Model, chitta.DefaultEmbedModelDir())
		if err != nil {
			chittaSys.SetEmbedder(embedder)
			chittaSys.StartEmbedWorker(embedder, 30*time.Second, func(format string, args ...any) {
				log.Printf("zbotd: "+format, args...)
			})
			log.Printf("zbotd: chitta embedding producer active (%s)", cfg.Chitta.Embedding.Model)
		} else {
			log.Printf("zbotd: chitta embedding producer off: %v", err)
		}
	} else {
		log.Printf("zbotd: chitta embedding producer off (cold cache — run `task setup:chitta`; keyword+graph recall)")
	}

	// Quick chat's BASE plane is EMPTY (operator direction: no clock,
	// no echo, no chitta tools on the lean surface) — the ward + MCP
	// planes mount per exchange in wireChatToolPlane below. Memory
	// rides the ## Context section, not tools.
	chatSvc := chat.NewStoreBacked(db, provStore, providers.NewChatClient(), emptyProvider{}, bus, nil)

	// Ward vault: user data OUTSIDE ~/.zbot (default ~/Documents/wards,
	// config wards.vault_root override). Bootstrap creates it and seeds
	// the archetype templates write-once; the daemon then only reads it.
	vaultRoot, err := wards.ResolveVaultRoot(cfg.Wards.VaultRoot, home)
	if err != nil {
		return err // names config.yaml: wards.vault_root and the reason
	}
	if err := wards.Bootstrap(ctx, vaultRoot); err != nil {
		return fmt.Errorf("ward vault bootstrap: %w", err)
	}
	wardVault, err := wards.Open(ctx, vaultRoot)
	if err != nil {
		return fmt.Errorf("ward vault open: %w", err)
	}
	// Ward v2: existing wards gain the new homes additively
	// (wiki/reports/apps + .zbot/config.yaml; nothing moves).
	if err := wards.MigrateV2(ctx, vaultRoot); err != nil {
		return fmt.Errorf("ward vault v2 migration: %w", err)
	}

	// Composed system prompt: static sections for BOTH surfaces (chat
	// is static-only; goal runs add dynamic sections in the wrapper).
	composePersona := func() (string, error) {
		return sysprompt.Compose(cfg.SystemPrompt, reg.Snapshot())
	}
	// The lean persona + memory recall (the session model: quick chat is
	// persona + Context and nothing more; the research discipline joins
	// via the wrapper's composed prompt).
	wireLeanChat(chatSvc, cfg, reg, chittaSys)

	// Engine: eino adapter over the provider plane + store + builtins;
	// the registry snapshot supplies agent definitions (delegation).
	eng := einoengine.New(provStore, db, bus, allTools, reg.Snapshot)
	// Ward hosting (slice 3): root goal runs gain the ward tool plane
	// (ward_use/ward_create + ward-scoped file tools) with a sticky
	// per-session active ward.
	eng.SetWardVault(func() *wards.Vault { return wardVault })

	// Resource index: registry snapshot + probed MCP tools → chitta
	// reserved kinds; reconciled on boot (the coalescing worker's
	// initial signal) and on every registry change.
	resIndex := &resourceindex.Indexer{
		Sys:  chittaSys,
		Host: domain.ChittaHostContext{Source: domain.SourceDistiller, AgentID: "resource-index"},
	}

	// Intent middleware wraps the engine for ROOT goal runs (router →
	// analysis → composed prompt + step budget; children/chat/Resume
	// pass through). Disabled by config → the plain engine.
	runEngine := domain.AgentEngine(eng)
	if cfg.Intent.On() {
		// The analysis model: cfg intent_analysis.model as "provider/model"
		// when set; otherwise the completer's default provider (absent
		// one, analysis degrades non-fatally with the reason in the event).
		var analysisProvider, analysisModel string
		if cfg.Intent.Model != "" {
			analysisProvider, analysisModel, _ = strings.Cut(cfg.Intent.Model, "/")
		}
		runEngine = &intent.Wrapper{
			Engine: eng,
			Analyzer: &intent.Analyzer{
				Completer: llmCaller{provStore, providers.NewChatClient()},
				Snapshot:  reg.Snapshot,
				Provider:  analysisProvider,
				Model:     analysisModel,
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
				page, err := chittaSys.Recall(ctx, domain.RecallRequest{Query: goal, Limit: 8}, domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
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
				Enabled:        cfg.Intent.On(),
				AnalysisModel:  cfg.Intent.Model,
				ComposePersona: composePersona,
				Persist: func(ev domain.TimelineEvent) {
					// Attempted-durable-first, the engine's own posture.
					// The stored payload carries text AND the typed
					// analysis (the SSE replay reads both).
					payload, _ := json.Marshal(map[string]any{
						"text":            ev.Text,
						"intent_analysis": ev.IntentAnalysis,
					})
					_ = db.AppendEvent(zbotstore.Event{ //nolint:errcheck // attempted-durable-first (F-15 posture)
						ID: ev.ID, SessionID: ev.SessionID, Kind: string(ev.Kind), Payload: string(payload),
					})
					_ = bus.Publish(context.Background(), ev) //nolint:errcheck // fire-and-forget emit (F-15)
				},
				RecallResources: func(ctx context.Context, goal string) intent.Resources {
					hits, err := resIndex.Relevant(ctx, goal, 12)
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
					known, err := resIndex.KnownMCP(ctx, name)
					return err == nil && known
				},
			},
		}
	}

	// The retrieval plane (ward-v2): configured stdio MCP servers stay
	// connected; root engine runs mount their tools live (search!).
	mcpExec := wireRetrievalPlane(ctx, eng, reg.Snapshot)

	// Quick chat's tool plane (operator direction): ward read/write/
	// shell + the live MCP tools — no clock/echo/chitta tools.
	wireChatToolPlane(chatSvc, wardVault, mcpExec)

	// The session model (add-chat-modes): research sessions run the
	// engine pipeline in their own transcript, and ward enters update
	// the session-row binding.
	wireSessionModel(chatSvc, eng, runEngine, db)

	// Model catalog: warm from the store, persist every refresh.
	catalog := restoreCatalog(db)

	// An operator-selected non-loopback bind adapts the request guard
	// (Host enforcement is a loopback-posture defense; see guard.go).
	if bindHost, _, err := net.SplitHostPort(cfg.Server.Address); err == nil {
		ip := net.ParseIP(bindHost)
		exposed := bindHost == "0.0.0.0" || bindHost == "::" || (ip != nil && !ip.IsLoopback())
		server.SetExposedBind(exposed)
	}

	// Scheduler: cron-fired goal runs over the registry + engine —
	// the WRAPPED engine, so scheduled root goals get the middleware
	// like every other goal run.
	sched := scheduler.New(reg, runEngine, bus, func() string { return fmt.Sprintf("evt-sched-%d", time.Now().UnixNano()) }, nil, filepath.Join(paths.DataDir, "scheduler-state.json"))
	daemonSrv := server.New(provStore, providers.NewClient(), catalog, chatSvc, runEngine, bus, db, reg, mcpclient.GoSDKProber{}, sched, wardVault, version)
	// The OpenAI-compatible channel's optional bearer guard
	// (server.api_token; empty = loopback posture).
	daemonSrv.SetV1Token(cfg.Server.ApiToken)
	daemonSrv.SetSkillHub(skillhub.NewHTTPIndexClient(), skillhub.NewGitHubRepoFetcher(), skillStore)
	// Probed MCP tools flow into the resource index (bounded context:
	// indexing is best-effort, never blocks the probe response).
	daemonSrv.SetMCPToolIndexer(func(server string, tools []mcpclient.Tool) {
		infos := make([]resourceindex.ToolInfo, len(tools))
		for i, t := range tools {
			infos[i] = resourceindex.ToolInfo{Name: t.Name, Description: t.Description}
		}
		indexCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := resIndex.IndexTools(indexCtx, server, infos); err != nil {
			log.Printf("zbotd: mcp tool index failed for %s: %v", server, err)
		}
	})

	srv := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           daemonSrv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM: in-flight requests drain,
	// then the daemon exits. Provider config changes apply on restart.
	go sched.Run(ctx)

	// Distiller: sessions → chitta (trusted seam, source: distiller).
	// Settings re-read each pass from the daemon-managed
	// distillation.json (Settings → Advanced); ticker-only in v1.
	go distiller.New(chittaSys, storeSource{db}, llmCaller{provStore, providers.NewChatClient()},
		&distiller.FileSettings{Dir: paths.ConfigDir("")},
		func(format string, args ...any) { log.Printf("zbotd: "+format, args...) }).
		Run(ctx)

	// Registry hot-reload: external config edits land without a restart.
	watcher, err := reg.Watch(ctx, registry.Dirs{
		Prompts:    paths.ConfigDir("prompts"),
		Agents:     paths.AgentsDir,
		Skills:     paths.SkillsDir,
		Hooks:      paths.HooksDir,
		MCPs:       paths.ConfigDir("mcps"),
		Schedules:  paths.ConfigDir("schedules"),
		ConfigFile: paths.ConfigFile,
	})
	if err != nil {
		return fmt.Errorf("registry watch: %w", err)
	}
	// Resource index follows the registry: every change SIGNALS a
	// coalescing worker (the watcher callback must not block — sync
	// does chitta I/O; the worker takes the latest snapshot).
	indexDirty := make(chan struct{}, 1)
	indexDirty <- struct{}{} // register before startup sync: no gap at the seam
	go func() {
		for range indexDirty {
			if err := resIndex.Sync(ctx, reg.Snapshot()); err != nil {
				log.Printf("zbotd: resource index sync failed: %v", err)
			}
		}
	}()
	watcher.OnChange(func(registry.Change) {
		mcpExec.Refresh(context.WithoutCancel(ctx)) // live tools follow the registry
		select {                                    // coalesce: one pending signal is enough
		case indexDirty <- struct{}{}:
		default:
		}
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("zbotd %s listening on %s (home %s, %d provider(s))", version, cfg.Server.Address, home, len(cfg.Providers))

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// llmCaller adapts the provider plane to the distiller's LLMCaller.
type llmCaller struct {
	store  *providers.Store
	client *providers.ChatClient
}

func (l llmCaller) Complete(ctx context.Context, provider, model, system, user string) (string, error) {
	return l.CompleteWith(ctx, provider, model, system, user, nil)
}

// CompleteWith carries extra request-body parameters (the intent
// analyzer's provider knobs, e.g. z.ai reasoning_effort=low).
func (l llmCaller) CompleteWith(ctx context.Context, provider, model, system, user string, extra map[string]any) (string, error) {
	p, token, err := l.store.ResolvedLookup(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("resolve provider %s: %w", provider, err)
	}
	messages := []providers.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	resp, err := l.client.ChatCompletionWithBody(ctx, p, model, token, messages, nil, extra)
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}
