package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CatalogModel is one persisted catalog entry: the model id plus its
// context window when the provider reported it (0 = unknown).
type CatalogModel struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// CatalogEntry is one provider's persisted model list.
type CatalogEntry struct {
	Provider    string
	Models      []CatalogModel
	RefreshedAt time.Time
}

// SaveCatalogEntry upserts one provider's model list (called by the
// Catalog's persistence hook after every successful refresh).
func (s *Store) SaveCatalogEntry(provider string, models []CatalogModel, refreshedAt time.Time) error {
	raw, err := json.Marshal(models)
	if err != nil {
		return fmt.Errorf("store: marshal catalog: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO model_catalog (provider, model_ids, refreshed_at)
		VALUES (?, ?, ?) ON CONFLICT(provider) DO UPDATE SET model_ids = excluded.model_ids, refreshed_at = excluded.refreshed_at`,
		provider, string(raw), fmtTime(refreshedAt))
	if err != nil {
		return fmt.Errorf("store: save catalog entry: %w", err)
	}
	return nil
}

// LoadCatalog returns every persisted catalog entry (called once at
// daemon startup to warm the in-memory catalog — no provider requests).
// Rows written before windows existed hold a plain string list; both
// shapes decode.
func (s *Store) LoadCatalog() ([]CatalogEntry, error) {
	rows, err := s.db.Query(`SELECT provider, model_ids, refreshed_at FROM model_catalog`)
	if err != nil {
		return nil, fmt.Errorf("store: load catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CatalogEntry
	for rows.Next() {
		var e CatalogEntry
		var idsJSON, refreshed string
		if err := rows.Scan(&e.Provider, &idsJSON, &refreshed); err != nil {
			return nil, fmt.Errorf("store: catalog scan: %w", err)
		}
		e.Models = decodeCatalogModels(idsJSON)
		e.RefreshedAt, _ = time.Parse(time.RFC3339Nano, refreshed)
		out = append(out, e)
	}
	return out, rows.Err()
}

// decodeCatalogModels reads the persisted shape: [{id, context_window}]
// or the legacy ["id", ...].
func decodeCatalogModels(raw string) []CatalogModel {
	var models []CatalogModel
	if json.Unmarshal([]byte(raw), &models) == nil {
		return models
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	models = make([]CatalogModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, CatalogModel{ID: id})
	}
	return models
}

var _ = sql.ErrNoRows

// SetDefaultModel persists the user's default model choice (the model
// the chat uses when the picker selection is saved).
func (s *Store) SetDefaultModel(qualified string) error {
	_, err := s.db.Exec(`INSERT INTO app_settings (key, value) VALUES ('default_model', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, qualified)
	if err != nil {
		return fmt.Errorf("store: set default model: %w", err)
	}
	return nil
}

// DefaultModel returns the stored default model ("" when unset).
func (s *Store) DefaultModel() (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = 'default_model'`).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: default model: %w", err)
	}
	return v, nil
}
