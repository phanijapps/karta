// Package server exposes the zbotd HTTP daemon: the contract surface in
// contracts/openapi.yaml, mounted from generated glue (internal/server/
// apigen). It imports config + providers and knows nothing about clients,
// the engine, or UI code.
//
// Endpoints whose owning change has not landed (goals, sessions, SSE)
// stay on the generated Unimplemented stubs (501) so clients can tell
// "planned by the contract" from "unknown route".
package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
)

// Server implements the daemon's contract operations. apigen.Unimplemented
// supplies 501 stubs for every operation not yet owned by a change.
type Server struct {
	apigen.Unimplemented

	store   *providers.Store
	client  *providers.Client
	catalog *providers.Catalog
	version string
}

// New builds a server over the provider store and model catalog.
// version lands in /healthz (zbotd injects its build version; "dev" in
// development).
func New(store *providers.Store, client *providers.Client, catalog *providers.Catalog, version string) *Server {
	return &Server{store: store, client: client, catalog: catalog, version: version}
}

// Routes mounts every contract operation (paths stay in lockstep with
// contracts/openapi.yaml via the generated glue) and returns the handler.
// Mutating methods pass the cross-origin guard first.
func (s *Server) Routes() http.Handler {
	return guardMutations(apigen.HandlerFromMux(s, chi.NewRouter()))
}

// writeJSON emits one typed response body.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError emits the contract's generic Error body.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apigen.Error{Code: code, Message: message})
}

// GetHealth answers the liveness probe.
func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apigen.Health{
		Service: apigen.Zbotd,
		Status:  apigen.Ok,
		Version: s.version,
	})
}

// providerView maps a resolved provider to its contract shape. The
// credential status is resolved per request (deadline-bounded for
// keyring references); no credential VALUE ever crosses this boundary.
func (s *Server) providerView(ctx context.Context, p providers.Provider) apigen.Provider {
	credential := apigen.ProviderCredential(p.CredentialKind())
	view := apigen.Provider{
		Name:                 apigen.Id(p.Name),
		Type:                 apigen.ProviderType(p.Type),
		BaseUrl:              p.BaseURL,
		Origin:               apigen.ProviderOrigin(p.Origin),
		Credential:           &credential,
		CredentialStatus:     apigen.CredentialStatus(providers.CredentialStatusFor(ctx, p, s.store.Secrets())),
		CredentialConfigured: false,
	}
	if view.CredentialStatus == apigen.CredentialStatus(providers.CredentialReady) {
		view.CredentialConfigured = true
	}
	if p.APIKeyEnv != "" {
		name := p.APIKeyEnv
		view.ApiKeyEnv = &name
	}
	return view
}
