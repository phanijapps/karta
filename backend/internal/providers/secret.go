package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// CredentialStatus is the resolution state of a provider's credential.
type CredentialStatus string

const (
	CredentialUnset       CredentialStatus = "unset"       // no reference
	CredentialReady       CredentialStatus = "ready"       // value resolvable now
	CredentialMissing     CredentialStatus = "missing"     // referenced, unset or absent
	CredentialUnavailable CredentialStatus = "unavailable" // keyring unreachable within its deadline
)

// Secret-store errors. ErrSecretNotFound means the item is absent (a
// missing credential, not a failure); ErrKeyringUnavailable means the
// platform keyring could not be reached within its deadline.
var (
	ErrSecretNotFound     = errors.New("providers: secret not found in keyring")
	ErrKeyringUnavailable = errors.New("providers: keyring unavailable")
)

// SecretStore stores per-provider credential values in the platform
// keyring, namespaced by the resolved home root so relocated homes never
// share secrets. Implementations must enforce the call deadline.
type SecretStore interface {
	Set(ctx context.Context, name, value string) error
	Get(ctx context.Context, name string) (string, error)
	Delete(ctx context.Context, name string) error
}

// secretDeadline bounds every keyring call so a stalled Secret Service
// cannot stall listings or discovery.
const secretDeadline = 3 * time.Second

// keyringService is the product identity shown in the OS credential
// manager (user-facing surface: Karta).
const keyringService = "Karta"

// KeyringStore is the production SecretStore over zalando/go-keyring,
// with per-name addresses namespaced by the home root
// (provider:<name>@<homeHash>). A store-level mutex fences the underlying
// blocking operations: a call whose caller timed out keeps running, but
// every later operation on this store queues behind it — a late-completing
// write can never land after a newer one (P0 review finding).
type KeyringStore struct {
	homeNS string
	opMu   sync.Mutex
	// ops is the keyring implementation (injectable for fence tests).
	ops keyringOps
}

// keyringOps is the slice of go-keyring the store depends on.
type keyringOps struct {
	set    func(service, user, password string) error
	get    func(service, user string) (string, error)
	delete func(service, user string) error
}

// NewKeyringStore builds the keyring store for one resolved home root.
func NewKeyringStore(home string) *KeyringStore {
	sum := sha256.Sum256([]byte(home))
	return &KeyringStore{
		homeNS: hex.EncodeToString(sum[:])[:8],
		ops:    keyringOps{set: keyring.Set, get: keyring.Get, delete: keyring.Delete},
	}
}

func (k *KeyringStore) user(name string) string {
	return fmt.Sprintf("provider:%s@%s", name, k.homeNS)
}

// call runs a blocking keyring operation under the store deadline.
func call[T any](ctx context.Context, op func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		val, err := op()
		done <- result{val, err}
	}()
	timer := time.NewTimer(secretDeadline)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.val, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-timer.C:
		var zero T
		return zero, ErrKeyringUnavailable
	}
}

// Set stores a provider credential value.
func (k *KeyringStore) Set(ctx context.Context, name, value string) error {
	_, err := call(ctx, func() (struct{}, error) {
		k.opMu.Lock()
		defer k.opMu.Unlock()
		return struct{}{}, k.ops.set(keyringService, k.user(name), value)
	})
	return mapKeyringErr(err)
}

// Get retrieves a provider credential value.
func (k *KeyringStore) Get(ctx context.Context, name string) (string, error) {
	val, err := call(ctx, func() (string, error) {
		k.opMu.Lock()
		defer k.opMu.Unlock()
		return k.ops.get(keyringService, k.user(name))
	})
	if err != nil {
		return "", mapKeyringErr(err)
	}
	return val, nil
}

// Delete removes a provider credential value.
func (k *KeyringStore) Delete(ctx context.Context, name string) error {
	_, err := call(ctx, func() (struct{}, error) {
		k.opMu.Lock()
		defer k.opMu.Unlock()
		return struct{}{}, k.ops.delete(keyringService, k.user(name))
	})
	return mapKeyringErr(err)
}

// mapKeyringErr distinguishes a missing item from an unreachable keyring.
func mapKeyringErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrSecretNotFound
	}
	return fmt.Errorf("%w: %v", ErrKeyringUnavailable, err)
}

// ResolveCredential resolves a provider's credential to a bearer token
// for one request. A referenced-but-missing credential yields an empty
// token (requests are sent unauthenticated, status missing); an
// unreachable keyring yields ErrKeyringUnavailable so callers fail typed
// instead of silently sending an unauthenticated request.
func ResolveCredential(ctx context.Context, p Provider, secrets SecretStore) (string, error) {
	switch p.CredentialKind() {
	case CredentialEnv:
		return osGetenv(p.APIKeyEnv), nil // empty when unset → missing
	case CredentialKeyring:
		val, err := secrets.Get(ctx, p.Name)
		if errors.Is(err, ErrSecretNotFound) {
			return "", nil // missing → send without credential
		}
		if err != nil {
			return "", err // unavailable → typed failure
		}
		return val, nil
	default:
		return "", nil
	}
}

// CredentialStatusFor computes the listing status of a provider's
// credential. Keyring probes are deadline-bounded; a failed probe maps to
// unavailable (never a stall and never a fallback).
func CredentialStatusFor(ctx context.Context, p Provider, secrets SecretStore) CredentialStatus {
	switch p.CredentialKind() {
	case CredentialEnv:
		if osGetenv(p.APIKeyEnv) == "" {
			return CredentialMissing
		}
		return CredentialReady
	case CredentialKeyring:
		_, err := secrets.Get(ctx, p.Name)
		switch {
		case err == nil:
			return CredentialReady
		case errors.Is(err, ErrSecretNotFound):
			return CredentialMissing
		default:
			return CredentialUnavailable
		}
	default:
		return CredentialUnset
	}
}
