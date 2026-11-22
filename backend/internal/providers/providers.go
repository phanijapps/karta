// Package providers resolves configured model providers (from config.yaml
// and the managed config/providers/ directory), performs OpenAI-compatible
// model discovery against them, and — via Store — manages the provider
// lifecycle (create/edit/delete with OS-keyring credentials). It sits
// beside internal/config in the dependency graph and never imports the
// HTTP server or any client code.
//
// Credentials are referenced by environment-variable name or the OS
// keyring; values live only in memory for request or status use and are
// never stored under ~/.zbot/, returned, or logged.
package providers

import (
	"errors"
	"fmt"

	"zbot/backend/internal/config"
)

// ErrUnknownProvider reports a name that is not present in the configured
// provider list. It is distinct from any connection or authentication
// failure so callers can map it to a different contract status.
var ErrUnknownProvider = errors.New("providers: unknown provider")

// Origin says where a provider is declared.
type Origin string

const (
	// OriginConfig — hand-authored in config.yaml (read-only via API).
	OriginConfig Origin = "config"
	// OriginFile — daemon-managed under config/providers/ (CRUD-able).
	OriginFile Origin = "file"
)

// CredentialKind mirrors the contract's credential reference on a
// provider listing: none, env (api_key_env), or keyring (api_key marker).
type CredentialKind string

const (
	// CredentialNone — CredentialKind values — how a provider references its credential.
	CredentialNone CredentialKind = "none"
	// CredentialEnv — one of the CredentialKind values.
	CredentialEnv CredentialKind = "env"
	// CredentialKeyring — one of the CredentialKind values.
	CredentialKeyring CredentialKind = "keyring"
)

// Provider is one resolved provider entry. CredentialKind() says how its
// credential is referenced; the value is resolved per request.
type Provider struct {
	Name       string
	Type       string
	BaseURL    string
	APIKeyEnv  string // env-var NAME when CredentialKind() == env
	UseKeyring bool   // api_key: keyring marker (file origin only)
	Origin     Origin
}

// CredentialKind reports how this provider references a credential.
func (p Provider) CredentialKind() CredentialKind {
	switch {
	case p.APIKeyEnv != "":
		return CredentialEnv
	case p.UseKeyring:
		return CredentialKeyring
	default:
		return CredentialNone
	}
}

// Set is an immutable resolved provider list, addressable by name.
type Set struct {
	byName map[string]Provider
	names  []string
}

// New builds a Set from the strictly-validated config list. Validation
// happened at config load, so New cannot fail; it is defensive only.
func New(list []config.ProviderConfig) *Set {
	s := &Set{byName: make(map[string]Provider, len(list))}
	for _, p := range list {
		entry := Provider{Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, APIKeyEnv: p.APIKeyEnv, Origin: OriginConfig}
		s.byName[p.Name] = entry
		s.names = append(s.names, p.Name)
	}
	return s
}

// Lookup resolves a provider by its configured name.
func (s *Set) Lookup(name string) (Provider, error) {
	p, ok := s.byName[name]
	if !ok {
		return Provider{}, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	return p, nil
}

// List returns every configured provider in declaration order.
func (s *Set) List() []Provider {
	out := make([]Provider, 0, len(s.names))
	for _, name := range s.names {
		out = append(out, s.byName[name])
	}
	return out
}
