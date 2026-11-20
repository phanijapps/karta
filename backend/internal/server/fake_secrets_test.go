package server

import (
	"context"
	"sync"

	"zbot/backend/internal/providers"
)

// fakeSecrets is the server tests' in-memory SecretStore (the providers
// package keeps its own; this one stays local to handler tests). No
// keyring access ever leaves the process. Failures wrap the real
// sentinels so handler mapping via errors.Is behaves exactly as in
// production.
type fakeSecrets struct {
	mu      sync.Mutex
	items   map[string]string
	failAll bool // when true every call fails as keyring-unavailable
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{items: map[string]string{}} }

func (f *fakeSecrets) Set(_ context.Context, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errKeyringDown()
	}
	f.items[name] = value
	return nil
}

func (f *fakeSecrets) Get(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return "", errKeyringDown()
	}
	val, ok := f.items[name]
	if !ok {
		return "", providers.ErrSecretNotFound
	}
	return val, nil
}

func (f *fakeSecrets) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errKeyringDown()
	}
	delete(f.items, name)
	return nil
}

func errKeyringDown() error {
	return providers.ErrKeyringUnavailable
}
