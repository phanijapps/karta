package server

import (
	"errors"
	"net/http"
	"time"

	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
)

// ListModels reads the daemon's model catalog: flat provider+id entries
// with per-provider refresh timestamps. Served from memory — this never
// contacts a provider, so an unreachable provider cannot slow it down.
// Entries are reconciled against the CURRENT provider set at read time:
// a deleted or renamed provider's stale entries and timestamps drop out
// no matter how the store changed (API, watcher, hand edit).
func (s *Server) ListModels(w http.ResponseWriter, r *http.Request) {
	configured := make(map[string]struct{}, 8)
	for _, p := range s.store.List() {
		configured[p.Name] = struct{}{}
	}
	snap := s.catalog.Snapshot()
	models := make([]apigen.ModelEntry, 0, len(snap.Models))
	for _, m := range snap.Models {
		if _, ok := configured[m.Provider]; !ok {
			continue
		}
		entry := apigen.ModelEntry{Provider: apigen.Id(m.Provider), Id: m.ID}
		if m.ContextWindow > 0 {
			entry.ContextWindow = &m.ContextWindow
		}
		models = append(models, entry)
	}
	body := apigen.ModelCatalog{Models: models}
	if len(snap.RefreshedAt) > 0 {
		refreshed := map[string]time.Time{}
		for name, ts := range snap.RefreshedAt {
			if _, ok := configured[name]; ok {
				refreshed[name] = ts
			}
		}
		if len(refreshed) > 0 {
			body.RefreshedAt = &refreshed
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// RefreshProviderModels live-fetches one provider's models, updates the
// catalog, and returns them. A failure keeps the previous catalog entry.
// The Settings Test connection exercises this same path, so every
// successful discovery warms the catalog.
func (s *Server) RefreshProviderModels(w http.ResponseWriter, r *http.Request, name apigen.ProviderName) {
	models, err := s.discoverModels(r.Context(), string(name))
	if err != nil {
		s.writeDiscoveryError(w, err)
		return
	}
	// An in-flight refresh can outlive its provider (deleted mid-fetch):
	// only catalogue models for a provider that still exists.
	if _, err := s.store.Lookup(string(name)); err == nil {
		s.catalog.Set(string(name), models)
	}
	writeJSON(w, http.StatusOK, apigen.ModelList{Models: contractModels(models)})
}

// contractModels maps discovered models to the contract shape, each
// with its context window when the provider reported one.
func contractModels(models []providers.Model) []apigen.Model {
	out := make([]apigen.Model, 0, len(models))
	for _, m := range models {
		entry := apigen.Model{Id: m.ID}
		if m.ContextWindow > 0 {
			entry.ContextWindow = &m.ContextWindow
		}
		out = append(out, entry)
	}
	return out
}

// writeDiscoveryError maps discovery failures for the read/refresh
// surfaces: 404 unknown provider; 502 credential-unavailable when the
// keyring cannot be resolved; 502 typed provider failure otherwise.
func (s *Server) writeDiscoveryError(w http.ResponseWriter, err error) {
	if providers.IsUnknown(err) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, providers.ErrKeyringUnavailable) {
		writeProviderError(w, http.StatusBadGateway, "credential_unavailable", "the stored credential could not be retrieved from the OS keyring (is it running and unlocked?)", apigen.ProviderFailureReason("credential_unavailable"))
		return
	}
	if reason := providers.ReasonOf(err); reason != "" {
		writeProviderError(w, http.StatusBadGateway, "provider_failure", err.Error(), apigen.ProviderFailureReason(reason))
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "model discovery failed")
}
