// Command zbotd is the local-first Karta daemon: it resolves the control
// plane, strictly loads config.yaml, and serves the contract surface on
// the configured loopback address. Wiring only — behavior lives in
// internal/*.
package main

import (
	"path/filepath"

	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"zbot/backend/chitta"
	"zbot/backend/distiller"
	"zbot/backend/internal/domain"

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

	einoengine "zbot/backend/internal/engine/eino"
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
		Skills:     paths.ConfigDir("skills"),
		Hooks:      paths.ConfigDir("hooks"),
		MCPs:       paths.ConfigDir("mcps"),
		Schedules:  paths.ConfigDir("schedules"),
		ConfigFile: paths.ConfigFile,
	}); len(errs) > 0 {
		for _, e := range errs {
			log.Printf("zbotd: registry: %v", e)
		}
	}

	// The eternal chat: local SQLite store + provider plane + builtins.
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

	chatSvc := chat.NewStoreBacked(db, provStore, providers.NewChatClient(), allTools, bus, nil)

	// Engine: eino adapter over the provider plane + store + builtins.
	// Engine: eino adapter over the provider plane + store + builtins;
	// the registry snapshot supplies agent definitions (delegation).
	eng := einoengine.New(provStore, db, bus, allTools, reg.Snapshot)

	// Model catalog: load persisted entries (no provider requests), then
	// persist every future refresh so restarts keep the catalog warm.
	catalog := providers.NewCatalog()
	if entries, err := db.LoadCatalog(); err == nil {
		for _, e := range entries {
			models := make([]providers.Model, 0, len(e.ModelIDs))
			for _, id := range e.ModelIDs {
				models = append(models, providers.Model{ID: id})
			}
			catalog.LoadEntry(e.Provider, models, e.RefreshedAt)
		}
		log.Printf("zbotd: model catalog restored (%d provider(s) from last refresh)", len(entries))
	}
	catalog.OnSet(func(provider string, models []providers.Model, refreshedAt time.Time) {
		ids := make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		if err := db.SaveCatalogEntry(provider, ids, refreshedAt); err != nil {
			log.Printf("zbotd: catalog persist for %q failed: %v", provider, err)
		}
	})

	// An operator-selected non-loopback bind adapts the request guard
	// (Host enforcement is a loopback-posture defense; see guard.go).
	if bindHost, _, err := net.SplitHostPort(cfg.Server.Address); err == nil {
		ip := net.ParseIP(bindHost)
		exposed := bindHost == "0.0.0.0" || bindHost == "::" || (ip != nil && !ip.IsLoopback())
		server.SetExposedBind(exposed)
	}

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

	// Scheduler: cron-fired goal runs over the registry + engine.
	sched := scheduler.New(reg, eng, bus, func() string { return fmt.Sprintf("evt-sched-%d", time.Now().UnixNano()) }, nil, filepath.Join(paths.DataDir, "scheduler-state.json"))
	daemonSrv := server.New(provStore, providers.NewClient(), catalog, chatSvc, eng, bus, db, reg, mcpclient.GoSDKProber{}, sched, wardVault, version)
	daemonSrv.SetSkillHub(skillhub.NewHTTPIndexClient(), skillhub.NewGitHubRepoFetcher(), skillStore)

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
	if _, err := reg.Watch(ctx, registry.Dirs{
		Prompts:    paths.ConfigDir("prompts"),
		Agents:     paths.AgentsDir,
		Skills:     paths.ConfigDir("skills"),
		Hooks:      paths.ConfigDir("hooks"),
		MCPs:       paths.ConfigDir("mcps"),
		Schedules:  paths.ConfigDir("schedules"),
		ConfigFile: paths.ConfigFile,
	}); err != nil {
		return fmt.Errorf("registry watch: %w", err)
	}

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

// storeSource adapts the conversation store to the distiller's
// SessionSource (kernel seam read; newest sessions first).
type storeSource struct{ db *zbotstore.Store }

func (s storeSource) FinishedSessions(ctx context.Context, limit int) ([]distiller.Session, error) {
	sessions, err := s.db.ListSessions(limit)
	if err != nil {
		return nil, err
	}
	var out []distiller.Session
	for _, sess := range sessions {
		msgs, err := s.db.Replay(sess.ID, 0, 500)
		if err != nil || len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		finished := time.Since(last.CreatedAt) >= distiller.IdleThreshold()
		ds := distiller.Session{ID: sess.ID, ExecutionID: last.ExecutionID, Finished: finished}
		for _, m := range msgs {
			ds.Messages = append(ds.Messages, distiller.SessionMessage{Role: string(m.Role), Content: m.Content})
		}
		out = append(out, ds)
	}
	return out, nil
}

// llmCaller adapts the provider plane to the distiller's LLMCaller.
type llmCaller struct {
	store  *providers.Store
	client *providers.ChatClient
}

func (l llmCaller) Complete(ctx context.Context, provider, model, system, user string) (string, error) {
	p, token, err := l.store.ResolvedLookup(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("resolve provider %s: %w", provider, err)
	}
	messages := []providers.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	resp, err := l.client.ChatCompletion(ctx, p, model, token, messages, nil)
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}
