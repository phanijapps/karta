package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
)

// ListProviders returns the merged provider set with origins and
// credential statuses. Statuses resolve concurrently (each keyring probe
// is deadline-bounded; serial probing could stall a listing for
// providers × deadline) while preserving declaration order. Credential
// VALUES never appear.
func (s *Server) ListProviders(w http.ResponseWriter, r *http.Request) {
	list := s.store.List()
	ctx := r.Context()
	views := make([]apigen.Provider, len(list))
	var wg sync.WaitGroup
	for i, p := range list {
		wg.Add(1)
		go func(i int, p providers.Provider) {
			defer wg.Done()
			views[i] = s.providerView(ctx, p)
		}(i, p)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, apigen.ProviderList{Providers: views})
}

// discoverModels is the one live-fetch path shared by discovery reads,
// the refresh endpoint, and the Settings Test connection: resolve the
// provider and its credential atomically against mutations, then fetch.
// The caller maps failures and warms the catalog on success.
func (s *Server) discoverModels(ctx context.Context, name string) ([]providers.Model, error) {
	p, token, err := s.store.ResolvedLookup(ctx, name)
	if err != nil {
		return nil, err // unknown provider or keyring-unavailable
	}
	return s.client.ListModels(ctx, p, token)
}

// ListProviderModels proxies OpenAI-compatible model discovery for one
// configured provider. Provider and credential resolve atomically
// against mutations; the daemon (not the client) dials the provider; a
// keyring credential that cannot be resolved fails typed — never a
// silent unauthenticated request. A successful fetch warms the catalog.
func (s *Server) ListProviderModels(w http.ResponseWriter, r *http.Request, name apigen.ProviderName) {
	models, err := s.discoverModels(r.Context(), string(name))
	if err != nil {
		s.writeDiscoveryError(w, err)
		return
	}
	s.catalog.Set(string(name), models)
	out := make([]apigen.Model, 0, len(models))
	for _, m := range models {
		out = append(out, apigen.Model{Id: m.ID})
	}
	writeJSON(w, http.StatusOK, apigen.ModelList{Models: out})
}

// CreateProvider writes a new daemon-managed provider file.
func (s *Server) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req apigen.ProviderCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return // decodeJSON already wrote the 400.
	}
	providerType := apigen.OpenaiCompatible
	if req.Type != nil && *req.Type != "" {
		providerType = *req.Type
	}
	entry := providers.Provider{
		Name:    string(req.Name),
		Type:    string(providerType),
		BaseURL: req.BaseUrl,
	}
	created, err := s.store.Create(r.Context(), entry, credentialInput(req.Credential))
	if err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.providerView(r.Context(), created))
}

// UpdateProvider edits a daemon-managed provider (rename, base URL,
// credential). Absent fields retain current values.
func (s *Server) UpdateProvider(w http.ResponseWriter, r *http.Request, name apigen.ProviderName) {
	var req apigen.ProviderUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	patch := providers.ProviderPatch{}
	if req.Name != nil {
		rename := string(*req.Name)
		patch.Name = &rename
	}
	if req.BaseUrl != nil {
		url := *req.BaseUrl
		patch.BaseURL = &url
	}
	patch.Credential = credentialInput(req.Credential)
	updated, err := s.store.Update(r.Context(), string(name), patch)
	if err != nil {
		s.writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.providerView(r.Context(), updated))
}

// DeleteProvider removes a daemon-managed provider and best-effort its
// keyring item.
func (s *Server) DeleteProvider(w http.ResponseWriter, r *http.Request, name apigen.ProviderName) {
	if err := s.store.Delete(r.Context(), string(name)); err != nil {
		s.writeMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// credentialInput converts the generated credential object; nil stays
// nil (retain semantics live in the store).
func credentialInput(c *apigen.CredentialInput) *providers.CredentialInput {
	if c == nil {
		return nil
	}
	out := &providers.CredentialInput{Kind: providers.CredentialKind(c.Kind)}
	if c.EnvName != nil {
		out.EnvName = *c.EnvName
	}
	if c.Key != nil {
		out.Key = *c.Key
	}
	return out
}

// decodeJSON decodes a request body into dst, writing a typed 400 on any
// malformed input.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return err
	}
	return nil
}

// writeMutationError maps typed store failures to contract statuses.
func (s *Server) writeMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, providers.ErrUnknownProvider):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, providers.ErrNameExists):
		writeError(w, http.StatusConflict, "duplicate_name", err.Error())
	case errors.Is(err, providers.ErrOriginProtected):
		writeError(w, http.StatusConflict, "origin_protected", err.Error())
	case errors.Is(err, providers.ErrCapacity):
		writeError(w, http.StatusConflict, "provider_limit", err.Error())
	case errors.Is(err, providers.ErrKeyringUnavailable):
		writeError(w, http.StatusServiceUnavailable, "keyring_unavailable", "the platform keyring is unavailable (is the keyring service running and unlocked?)")
	default:
		// Validation and everything else: a bad request naming the rule.
		writeError(w, http.StatusBadRequest, "invalid_provider", err.Error())
	}
}

// writeProviderError emits the contract's typed provider-failure body.
func writeProviderError(w http.ResponseWriter, status int, code, message string, reason apigen.ProviderFailureReason) {
	writeJSON(w, status, apigen.ProviderError{Code: code, Message: message, Reason: reason})
}

// compile-time keep: context used in providerView path through handlers.
var _ = context.Background
