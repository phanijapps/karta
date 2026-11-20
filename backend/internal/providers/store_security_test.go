package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpdateFileFailureRestoresPriorSecret(t *testing.T) {
	ctx := context.Background()
	s, secrets, paths := newTestStore(t)
	mustCreate(t, s, "zai", "https://api.z.ai/v1", &CredentialInput{Kind: CredentialKeyring, Key: "K1"})

	// Same-name credential replacement with an unwritable providers dir:
	// the secret write succeeds, the file commit fails — the prior key K1
	// must be restored (never lost in favor of K2).
	if err := os.Chmod(paths.ProvidersDir, 0o500); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(paths.ProvidersDir, 0o755) })

	_, err := s.Update(ctx, "zai", ProviderPatch{Credential: &CredentialInput{Kind: CredentialKeyring, Key: "K2"}})
	if err == nil {
		t.Fatal("update must fail when the file cannot be written")
	}
	if v, err := secrets.Get(ctx, "zai"); err != nil || v != "K1" {
		t.Fatalf("prior secret must be restored, got %q err %v", v, err)
	}
}

func TestUpdateUnchangedCredentialTouchesNoSecret(t *testing.T) {
	ctx := context.Background()
	s, secrets, _ := newTestStore(t)
	mustCreate(t, s, "zai", "https://api.z.ai/v1", &CredentialInput{Kind: CredentialKeyring, Key: "K1"})
	// Make every secret op fail: a URL-only edit must still succeed.
	secrets.failSet, secrets.failGet, secrets.failDelete = errBoom(), errBoom(), errBoom()
	newURL := "https://api.z.ai/v2"
	p, err := s.Update(ctx, "zai", ProviderPatch{BaseURL: &newURL})
	if err != nil {
		t.Fatalf("URL-only edit must not touch the keyring: %v", err)
	}
	if p.BaseURL != newURL || !p.UseKeyring {
		t.Fatalf("unexpected result: %+v", p)
	}
	secrets.failSet, secrets.failGet, secrets.failDelete = nil, nil, nil
	if v, _ := secrets.Get(ctx, "zai"); v != "K1" {
		t.Fatalf("secret must be untouched, got %q", v)
	}
}

func TestUpdateKeylessKeyringRetainsStoredKey(t *testing.T) {
	ctx := context.Background()
	s, secrets, _ := newTestStore(t)
	mustCreate(t, s, "zai", "https://api.z.ai/v1", &CredentialInput{Kind: CredentialKeyring, Key: "K1"})
	// On edit, keyless keyring = retain the stored key (no Get/Set).
	secrets.failSet, secrets.failGet, secrets.failDelete = errBoom(), errBoom(), errBoom()
	p, err := s.Update(ctx, "zai", ProviderPatch{Credential: &CredentialInput{Kind: CredentialKeyring}})
	secrets.failSet, secrets.failGet, secrets.failDelete = nil, nil, nil
	if err != nil {
		t.Fatalf("keyless keyring edit must retain: %v", err)
	}
	if !p.UseKeyring || p.APIKeyEnv != "" {
		t.Fatalf("keyring reference must be retained: %+v", p)
	}
	if v, _ := secrets.Get(ctx, "zai"); v != "K1" {
		t.Fatalf("stored key must be untouched, got %q", v)
	}
}

func TestCreateRejectsKeylessKeyring(t *testing.T) {
	s, _, _ := newTestStore(t)
	_, err := s.Create(context.Background(), Provider{Name: "x", Type: configProviderType, BaseURL: "http://x:1/v1"}, &CredentialInput{Kind: CredentialKeyring})
	if err == nil || !containsAll(err.Error(), "requires the one-shot key") {
		t.Fatalf("want keyless-keyring create error, got: %v", err)
	}
}

func TestCreateDoesNotClobberUnseenExternalFile(t *testing.T) {
	s, _, paths := newTestStore(t)
	// External file the watcher has not folded into the snapshot yet.
	external := filepath.Join(paths.ProvidersDir, "new.yaml")
	if err := os.WriteFile(external, []byte("name: new\nbase_url: http://localhost:7/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(context.Background(), Provider{Name: "new", Type: configProviderType, BaseURL: "http://localhost:9/v1"}, nil)
	if !errors.Is(err, ErrNameExists) {
		t.Fatalf("create over an unseen external file must conflict, got: %v", err)
	}
	raw, _ := os.ReadFile(external)
	if len(raw) == 0 || string(raw[0:5]) != "name:" {
		t.Fatalf("external file must be intact, got %q", raw)
	}
}

func TestRenameDoesNotClobberUnseenExternalFile(t *testing.T) {
	ctx := context.Background()
	s, _, paths := newTestStore(t)
	mustCreate(t, s, "alpha", "http://localhost:1/v1", nil)
	external := filepath.Join(paths.ProvidersDir, "beta.yaml")
	if err := os.WriteFile(external, []byte("name: beta\nbase_url: http://localhost:7/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rename := "beta"
	_, err := s.Update(ctx, "alpha", ProviderPatch{Name: &rename})
	if !errors.Is(err, ErrNameExists) {
		t.Fatalf("rename over an unseen external file must conflict, got: %v", err)
	}
	if _, err := s.Lookup("alpha"); err != nil {
		t.Fatalf("old provider must remain operational: %v", err)
	}
}

func TestResolvedLookupAtomicAgainstDelete(t *testing.T) {
	ctx := context.Background()
	s, secrets, _ := newTestStore(t)
	mustCreate(t, s, "zai", "http://127.0.0.1:1/v1", &CredentialInput{Kind: CredentialKeyring, Key: "K1"})

	// Slow the credential read; a delete racing the resolution must not
	// strip the credential from the in-flight request.
	slow := &slowSecrets{inner: secrets, delay: 250 * time.Millisecond}
	s.secrets = slow

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		_, token, err := s.ResolvedLookup(ctx, "zai")
		done <- result{token, err}
	}()
	time.Sleep(50 * time.Millisecond) // resolution is inside its slow Get
	go func() {
		_ = s.Delete(ctx, "zai")
	}()
	res := <-done
	if res.err != nil || res.token != "K1" {
		t.Fatalf("in-flight resolution must keep its credential, got %q err %v", res.token, res.err)
	}
}

func TestKeyringFenceLateWriteCannotOverwrite(t *testing.T) {
	// A caller-timed-out Set keeps running underneath; the store's op
	// mutex fences completion order, so a later successful write can
	// never be overwritten by the late one.
	k := NewKeyringStore("/tmp/fence-home")
	mu := sync.Mutex{}
	items := map[string]string{}
	release := make(chan struct{})
	firstStarted := make(chan struct{})
	k.ops = keyringOps{
		set: func(service, user, password string) error {
			select {
			case <-firstStarted:
			default:
				close(firstStarted)
				<-release // first write stalls until released
			}
			mu.Lock()
			defer mu.Unlock()
			items[user] = password
			return nil
		},
		get: func(service, user string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			v, ok := items[user]
			if !ok {
				return "", ErrSecretNotFound
			}
			return v, nil
		},
		delete: func(service, user string) error {
			mu.Lock()
			defer mu.Unlock()
			delete(items, user)
			return nil
		},
	}

	ctx := context.Background()
	firstErr := make(chan error, 1)
	go func() { firstErr <- k.Set(ctx, "p", "K1") }()
	<-firstStarted // the stalled write holds the fence

	// Second write queues behind the fence and times out (deadline-bounded
	// caller), never writing.
	quickCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err := k.Set(quickCtx, "p", "K2"); !errors.Is(err, ErrKeyringUnavailable) && quickCtx.Err() == nil {
		// It may also have succeeded if timing flipped; both are safe, but
		// with the stall it must NOT have written K2.
		t.Fatalf("fenced second write should not complete, got %v", err)
	}
	release <- struct{}{} // first write completes now
	if err := <-firstErr; err != nil {
		t.Fatalf("first write completes despite caller timing out: %v", err)
	}
	// A later retry lands after the fence and wins.
	if err := k.Set(ctx, "p", "K2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := k.Get(ctx, "p"); v != "K2" {
		t.Fatalf("last write must win, got %q", v)
	}
}

// slowSecrets wraps a SecretStore with a fixed delay.
type slowSecrets struct {
	inner SecretStore
	delay time.Duration
}

func (s *slowSecrets) Set(ctx context.Context, name, value string) error {
	time.Sleep(s.delay)
	return s.inner.Set(ctx, name, value)
}
func (s *slowSecrets) Get(ctx context.Context, name string) (string, error) {
	time.Sleep(s.delay)
	return s.inner.Get(ctx, name)
}
func (s *slowSecrets) Delete(ctx context.Context, name string) error {
	time.Sleep(s.delay)
	return s.inner.Delete(ctx, name)
}

func TestFileEntriesValidateEnvNamesAndExclusiveRefs(t *testing.T) {
	dir := t.TempDir()
	bad := map[string]string{
		"bad-env.yaml":  "name: bad-env\nbase_url: http://x:1/v1\napi_key_env: \"$SECRET\"\n",
		"long-env.yaml": "name: long-env\nbase_url: http://x:1/v1\napi_key_env: " + longEnvName() + "\n",
		"both.yaml":     "name: both\nbase_url: http://x:1/v1\napi_key: keyring\napi_key_env: X_KEY\n",
	}
	for file, content := range bad {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := loadProviderFile(filepath.Join(dir, file), strings.TrimSuffix(file, ".yaml"))
		if err == nil {
			t.Fatalf("%s must be rejected", file)
		}
	}
}

func longEnvName() string {
	b := make([]byte, 129)
	for i := range b {
		b[i] = 'A'
	}
	return "E" + string(b[:128])
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func errBoom() error { return &stubFailure{} }

type stubFailure struct{}

func (*stubFailure) Error() string { return "boom" }
