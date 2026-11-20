package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// fakeSecrets is the in-memory SecretStore for tests, with injectable
// failures and stalls. No keyring access ever leaves the process.
type fakeSecrets struct {
	mu         sync.Mutex
	items      map[string]string
	failSet    error
	failGet    error
	failDelete error
	stall      time.Duration
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{items: map[string]string{}} }

func (f *fakeSecrets) Set(_ context.Context, name, value string) error {
	if f.stall > 0 {
		time.Sleep(f.stall)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSet != nil {
		return f.failSet
	}
	f.items[name] = value
	return nil
}

func (f *fakeSecrets) Get(_ context.Context, name string) (string, error) {
	if f.stall > 0 {
		time.Sleep(f.stall)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failGet != nil {
		return "", f.failGet
	}
	val, ok := f.items[name]
	if !ok {
		return "", ErrSecretNotFound
	}
	return val, nil
}

func (f *fakeSecrets) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete != nil {
		return f.failDelete
	}
	if _, ok := f.items[name]; !ok {
		return ErrSecretNotFound
	}
	delete(f.items, name)
	return nil
}

func TestResolveCredential(t *testing.T) {
	ctx := context.Background()
	secrets := newFakeSecrets()

	t.Run("env reference resolves and missing maps to empty", func(t *testing.T) {
		t.Setenv("PROV_TEST_KEY", "value")
		token, err := ResolveCredential(ctx, Provider{APIKeyEnv: "PROV_TEST_KEY"}, secrets)
		if err != nil || token != "value" {
			t.Fatalf("want env value, got %q err %v", token, err)
		}
		token, err = ResolveCredential(ctx, Provider{APIKeyEnv: "PROV_TEST_UNSET"}, secrets)
		if err != nil || token != "" {
			t.Fatalf("unset env must resolve empty (missing), got %q err %v", token, err)
		}
	})

	t.Run("keyring missing maps to empty token", func(t *testing.T) {
		token, err := ResolveCredential(ctx, Provider{Name: "ghost", UseKeyring: true}, secrets)
		if err != nil || token != "" {
			t.Fatalf("missing keyring item must send no credential, got %q err %v", token, err)
		}
	})

	t.Run("keyring value resolves", func(t *testing.T) {
		if err := secrets.Set(ctx, "zai", "k1"); err != nil {
			t.Fatal(err)
		}
		token, err := ResolveCredential(ctx, Provider{Name: "zai", UseKeyring: true}, secrets)
		if err != nil || token != "k1" {
			t.Fatalf("want k1, got %q err %v", token, err)
		}
	})

	t.Run("keyring unavailable is typed", func(t *testing.T) {
		failing := newFakeSecrets()
		failing.failGet = ErrKeyringUnavailable
		_, err := ResolveCredential(ctx, Provider{Name: "zai", UseKeyring: true}, failing)
		if !errors.Is(err, ErrKeyringUnavailable) {
			t.Fatalf("want ErrKeyringUnavailable, got: %v", err)
		}
	})

	t.Run("no reference resolves empty", func(t *testing.T) {
		token, err := ResolveCredential(ctx, Provider{Name: "ollama"}, secrets)
		if err != nil || token != "" {
			t.Fatalf("want empty, got %q err %v", token, err)
		}
	})
}

func TestCredentialStatusFor(t *testing.T) {
	ctx := context.Background()
	secrets := newFakeSecrets()
	_ = secrets.Set(ctx, "zai", "k1")

	cases := []struct {
		name     string
		provider Provider
		want     CredentialStatus
	}{
		{"unset", Provider{Name: "x"}, CredentialUnset},
		{"env ready", Provider{APIKeyEnv: "PROV_STATUS_SET"}, CredentialReady},
		{"env missing", Provider{APIKeyEnv: "PROV_STATUS_UNSET"}, CredentialMissing},
		{"keyring ready", Provider{Name: "zai", UseKeyring: true}, CredentialReady},
		{"keyring missing", Provider{Name: "ghost", UseKeyring: true}, CredentialMissing},
	}
	t.Setenv("PROV_STATUS_SET", "v")
	for _, tc := range cases {
		if got := CredentialStatusFor(ctx, tc.provider, secrets); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}

	failing := newFakeSecrets()
	failing.failGet = ErrKeyringUnavailable
	if got := CredentialStatusFor(ctx, Provider{Name: "zai", UseKeyring: true}, failing); got != CredentialUnavailable {
		t.Fatalf("unavailable keyring: got %s, want unavailable", got)
	}
}

func TestCallDeadlineBoundsStalledKeyring(t *testing.T) {
	start := time.Now()
	_, err := call(context.Background(), func() (struct{}, error) {
		time.Sleep(secretDeadline + 2*time.Second)
		return struct{}{}, nil
	})
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("want deadline failure, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > secretDeadline+500*time.Millisecond {
		t.Fatalf("deadline not enforced: %v", elapsed)
	}
}

func TestKeyringStoreHomeNamespace(t *testing.T) {
	a := NewKeyringStore("/tmp/zbot-home-a")
	b := NewKeyringStore("/tmp/zbot-home-b")
	if a.user("ollama") == b.user("ollama") {
		t.Fatal("different homes must not share keyring addresses")
	}
	if !stringsContains(a.user("ollama"), "provider:ollama@") {
		t.Fatalf("address format: %q", a.user("ollama"))
	}

	// End-to-end isolation through the real keyring implementation in
	// mock mode (no Secret Service involved).
	keyring.MockInit()
	ctx := context.Background()
	if err := a.Set(ctx, "ollama", "key-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, "ollama"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("home B must not see home A's secret, got: %v", err)
	}
	if v, err := a.Get(ctx, "ollama"); err != nil || v != "key-a" {
		t.Fatalf("home A round-trip: %q %v", v, err)
	}
	if err := a.Delete(ctx, "ollama"); err != nil {
		t.Fatal(err)
	}
}

func stringsContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && haystack[0:len(needle)] == needle
}
