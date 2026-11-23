package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"zbot/backend/internal/config"
)

// newTestStore builds a Store over a temp home with a fake secret store.
func newTestStore(t *testing.T, cfgProviders ...config.ProviderConfig) (*Store, *fakeSecrets, config.Paths) {
	t.Helper()
	home := t.TempDir()
	paths := config.NewPaths(home)
	if err := config.Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Providers = cfgProviders
	secrets := newFakeSecrets()
	s, err := NewStore(paths, cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, secrets, paths
}

func mustCreate(t *testing.T, s *Store, name, baseURL string, cred *CredentialInput) Provider {
	t.Helper()
	p, err := s.Create(context.Background(), Provider{Name: name, Type: configProviderType, BaseURL: baseURL}, cred)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return p
}

func TestStoreMergeOrderAndOrigins(t *testing.T) {
	s, _, paths := newTestStore(t, config.ProviderConfig{Name: "from-config", Type: "openai-compatible", BaseURL: "http://localhost:1/v1"})
	mustCreate(t, s, "from-file", "http://localhost:2/v1", nil)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 providers, got %+v", list)
	}
	if list[0].Name != "from-config" || list[0].Origin != OriginConfig {
		t.Fatalf("config.yaml entries come first with config origin: %+v", list[0])
	}
	if list[1].Name != "from-file" || list[1].Origin != OriginFile {
		t.Fatalf("file entries follow with file origin: %+v", list[1])
	}
	if _, err := os.Stat(filepath.Join(paths.ProvidersDir, "from-file.yaml")); err != nil {
		t.Fatalf("managed file missing: %v", err)
	}
}

func TestStoreDuplicateAcrossSources(t *testing.T) {
	s, _, _ := newTestStore(t, config.ProviderConfig{Name: "dup", Type: "openai-compatible", BaseURL: "http://localhost:1/v1"})
	_, err := s.Create(context.Background(), Provider{Name: "dup", Type: configProviderType, BaseURL: "http://localhost:2/v1"}, nil)
	if !errors.Is(err, ErrNameExists) {
		t.Fatalf("want ErrNameExists, got: %v", err)
	}
}

func TestStoreReloadRejectsExternalDuplicatesAndKeepsLastGood(t *testing.T) {
	s, _, paths := newTestStore(t, config.ProviderConfig{Name: "clash", Type: "openai-compatible", BaseURL: "http://localhost:1/v1"})
	// Hand-author a colliding file behind the store's back.
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, "clash.yaml"), []byte("name: clash\nbase_url: http://localhost:2/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := s.Reload()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate reload error, got: %v", err)
	}
	if len(s.List()) != 1 || s.List()[0].Name != "clash" {
		t.Fatalf("last good set must keep serving: %+v", s.List())
	}
	if s.LastError() == nil {
		t.Fatal("LastError must report the reload failure")
	}
}

func TestStoreCRUDValidation(t *testing.T) {
	s, _, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, Provider{Name: "Bad Name", Type: configProviderType, BaseURL: "http://x:1/v1"}, nil); err == nil {
		t.Fatal("unportable name must be rejected")
	}
	if _, err := s.Create(ctx, Provider{Name: "x", Type: "anthropic", BaseURL: "http://x:1/v1"}, nil); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("want type error, got: %v", err)
	}
	if _, err := s.Create(ctx, Provider{Name: "x", Type: configProviderType, BaseURL: "ftp://x"}, nil); err == nil {
		t.Fatal("invalid base_url must be rejected")
	}
	// Credential combos.
	for _, bad := range []CredentialInput{
		{Kind: CredentialNone, EnvName: "X"},
		{Kind: CredentialEnv},
		{Kind: CredentialEnv, EnvName: "X", Key: "k"},
		{Kind: CredentialKeyring},
		{Kind: CredentialKeyring, EnvName: "X"},
		{Kind: "bogus"},
	} {
		if _, err := s.Create(ctx, Provider{Name: "x", Type: configProviderType, BaseURL: "http://x:1/v1"}, &bad); err == nil {
			t.Fatalf("credential input %+v must be rejected", bad)
		}
	}
}

func TestStoreUpdateSemantics(t *testing.T) {
	ctx := context.Background()
	s, secrets, _ := newTestStore(t)
	mustCreate(t, s, "zai", "https://api.z.ai/v1", &CredentialInput{Kind: CredentialKeyring, Key: "k1"})

	t.Run("absent credential retains", func(t *testing.T) {
		newURL := "https://api.z.ai/v2"
		p, err := s.Update(ctx, "zai", ProviderPatch{BaseURL: &newURL})
		if err != nil {
			t.Fatal(err)
		}
		if p.BaseURL != newURL || !p.UseKeyring {
			t.Fatalf("credential must be retained: %+v", p)
		}
		if v, _ := secrets.Get(ctx, "zai"); v != "k1" {
			t.Fatalf("secret must be untouched, got %q", v)
		}
	})

	t.Run("rename re-keys and old name stops resolving", func(t *testing.T) {
		renamed := "zai-prod"
		p, err := s.Update(ctx, "zai", ProviderPatch{Name: &renamed})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "zai-prod" {
			t.Fatalf("renamed entry: %+v", p)
		}
		if _, err := s.Lookup("zai"); !errors.Is(err, ErrUnknownProvider) {
			t.Fatalf("old name must not resolve: %v", err)
		}
		if v, err := secrets.Get(ctx, "zai-prod"); err != nil || v != "k1" {
			t.Fatalf("secret must move to the new address: %q %v", v, err)
		}
		if _, err := secrets.Get(ctx, "zai"); !errors.Is(err, ErrSecretNotFound) {
			t.Fatalf("old secret must be cleaned up, got: %v", err)
		}
	})

	t.Run("clear credential deletes secret", func(t *testing.T) {
		p, err := s.Update(ctx, "zai-prod", ProviderPatch{Credential: &CredentialInput{Kind: CredentialNone}})
		if err != nil {
			t.Fatal(err)
		}
		if p.UseKeyring || p.APIKeyEnv != "" {
			t.Fatalf("credential must be cleared: %+v", p)
		}
		if _, err := secrets.Get(ctx, "zai-prod"); !errors.Is(err, ErrSecretNotFound) {
			t.Fatalf("secret must be gone, got: %v", err)
		}
	})

	t.Run("origin protection", func(t *testing.T) {
		s2, _, _ := newTestStore(t, config.ProviderConfig{Name: "cfg", Type: "openai-compatible", BaseURL: "http://localhost:1/v1"})
		rename := "whatever"
		if _, err := s2.Update(ctx, "cfg", ProviderPatch{Name: &rename}); !errors.Is(err, ErrOriginProtected) {
			t.Fatalf("want origin protection, got: %v", err)
		}
		if err := s2.Delete(ctx, "cfg"); !errors.Is(err, ErrOriginProtected) {
			t.Fatalf("want origin protection on delete, got: %v", err)
		}
	})
}

func TestStoreDelete(t *testing.T) {
	ctx := context.Background()
	s, secrets, paths := newTestStore(t)
	mustCreate(t, s, "tmp", "http://localhost:9/v1", &CredentialInput{Kind: CredentialKeyring, Key: "k"})
	if err := s.Delete(ctx, "tmp"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup("tmp"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("deleted provider must not resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProvidersDir, "tmp.yaml")); !os.IsNotExist(err) {
		t.Fatal("file must be removed")
	}
	if _, err := secrets.Get(ctx, "tmp"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("secret must be deleted, got: %v", err)
	}
	// Best-effort: a failing cleanup must not block the delete.
	mustCreate(t, s, "sticky", "http://localhost:9/v1", &CredentialInput{Kind: CredentialKeyring, Key: "k"})
	secrets.failDelete = fmt.Errorf("keyring down")
	if err := s.Delete(ctx, "sticky"); err != nil {
		t.Fatalf("delete must succeed despite cleanup failure: %v", err)
	}
	secrets.failDelete = nil
	if _, err := s.Lookup("sticky"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatal("sticky must be gone from the set")
	}
}

func TestStoreCreateCompensatesOnFileFailure(t *testing.T) {
	ctx := context.Background()
	s, secrets, paths := newTestStore(t)
	// Make the providers directory read-only: file commit will fail.
	if err := os.Chmod(paths.ProvidersDir, 0o500); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(paths.ProvidersDir, 0o755) })

	_, err := s.Create(ctx, Provider{Name: "comp", Type: configProviderType, BaseURL: "http://x:1/v1"}, &CredentialInput{Kind: CredentialKeyring, Key: "k"})
	if err == nil {
		t.Fatal("create must fail when the file cannot be written")
	}
	// The just-written secret must be compensated away.
	if _, err := secrets.Get(ctx, "comp"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("compensation must remove the secret, got: %v", err)
	}
}

func TestStoreKeyringUnavailableWritesNothing(t *testing.T) {
	ctx := context.Background()
	s, secrets, paths := newTestStore(t)
	secrets.failSet = ErrKeyringUnavailable
	_, err := s.Create(ctx, Provider{Name: "nokey", Type: configProviderType, BaseURL: "http://x:1/v1"}, &CredentialInput{Kind: CredentialKeyring, Key: "k"})
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("want typed keyring failure, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProvidersDir, "nokey.yaml")); !os.IsNotExist(err) {
		t.Fatal("no file may be written when the keyring is unavailable")
	}
	if _, err := s.Lookup("nokey"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatal("provider must not be published")
	}
}

func TestStoreCapacityAcrossSources(t *testing.T) {
	cfgProviders := make([]config.ProviderConfig, 0, config.MaxProviders)
	for i := 0; i < config.MaxProviders; i++ {
		cfgProviders = append(cfgProviders, config.ProviderConfig{Name: fmt.Sprintf("cfg%02d", i), Type: "openai-compatible", BaseURL: "http://localhost:1/v1"})
	}
	s, _, _ := newTestStore(t, cfgProviders...)
	_, err := s.Create(context.Background(), Provider{Name: "one-more", Type: configProviderType, BaseURL: "http://x:1/v1"}, nil)
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("want capacity error, got: %v", err)
	}
}

func TestStoreConcurrentMutationsAndReloads(t *testing.T) {
	s, _, _ := newTestStore(t)
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, _ = s.Create(context.Background(), Provider{Name: fmt.Sprintf("p%02d", i), Type: configProviderType, BaseURL: "http://localhost:1/v1"}, nil)
		}(i)
		go func() {
			defer wg.Done()
			_ = s.Reload()
		}()
	}
	wg.Wait()
	// A committed mutation is immediately visible even while reloads race.
	if got := len(s.List()); got != n {
		t.Fatalf("lost update: want %d providers, got %d", n, got)
	}
}

func TestStoreWatcherPicksUpExternalChanges(t *testing.T) {
	s, _, paths := newTestStore(t)
	// External create.
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, "external.yaml"), []byte("name: external\nbase_url: http://localhost:7/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "external provider appears", func() bool {
		_, err := s.Lookup("external")
		return err == nil
	})
	// External edit.
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, "external.yaml"), []byte("name: external\nbase_url: http://localhost:8/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edit picked up", func() bool {
		p, err := s.Lookup("external")
		return err == nil && p.BaseURL == "http://localhost:8/v1"
	})
	// External corrupt: last good set keeps serving.
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, "broken.yaml"), []byte("name: [bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "corruption surfaced", func() bool { return s.LastError() != nil })
	if _, err := s.Lookup("external"); err != nil {
		t.Fatalf("last good set must keep serving: %v", err)
	}
	// Repair: remove the broken file; reloads go green again.
	if err := os.Remove(filepath.Join(paths.ProvidersDir, "broken.yaml")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "store healthy again", func() bool { return s.LastError() == nil })
	// External delete.
	if err := os.Remove(filepath.Join(paths.ProvidersDir, "external.yaml")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "delete picked up", func() bool {
		_, err := s.Lookup("external")
		return errors.Is(err, ErrUnknownProvider)
	})
}

// waitFor polls a condition with a bounded timeout (watcher debounce).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
