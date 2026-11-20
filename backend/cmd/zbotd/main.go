// Command zbotd is the local-first Karta daemon: it resolves the control
// plane, strictly loads config.yaml, and serves the contract surface on
// the configured loopback address. Wiring only — behavior lives in
// internal/*.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"zbot/backend/internal/config"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/server"
)

// version is stamped via -ldflags "-X main.version=..." in releases.
var version = "dev"

func main() {
	homeFlag := flag.String("home", "", "z-Bot home root (default ~/.zbot; ZBOT_HOME also honored)")
	flag.Parse()

	if err := run(*homeFlag); err != nil {
		log.Fatalf("zbotd: %v", err)
	}
}

func run(homeFlag string) error {
	home, err := config.ResolveHome(homeFlag)
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	paths := config.NewPaths(home)
	if err := config.Bootstrap(paths); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err // Load errors already name config.yaml and the detail.
	}

	// The store merges config.yaml entries with the managed
	// config/providers/ directory and watches it for external changes.
	store, err := providers.NewStore(paths, cfg, providers.NewKeyringStore(home))
	if err != nil {
		return fmt.Errorf("providers: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           server.New(store, providers.NewClient(), providers.NewCatalog(), version).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	defer store.Close() // stops the providers watcher on any exit path

	// Graceful shutdown on SIGINT/SIGTERM: in-flight requests drain,
	// then the daemon exits. Provider config changes apply on restart.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("zbotd %s listening on %s (home %s, %d provider(s))", version, cfg.Server.Address, home, len(cfg.Providers))

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
