// Package store — the session-model mutators: titles, the sticky ward
// binding, and the per-ward warden ensure (add-chat-modes /
// ward-v2). Extracted from store.go (LOC budget); same package.
package store

import (
	"fmt"
)

// SetSessionTitleIfUntitled names a session only when it has no title
// (auto-titling never overwrites a user's name). Returns whether it
// titled.
func (s *Store) SetSessionTitleIfUntitled(sessionID, title string) (bool, error) {
	res, err := s.db.Exec(`UPDATE sessions SET title = ? WHERE id = ? AND (title IS NULL OR title = '')`, title, sessionID)
	if err != nil {
		return false, fmt.Errorf("store: auto-title: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: auto-title rows: %w", err)
	}
	return n > 0, nil
}

// EnsureWardenSession ensures the ward's persistent home session
// exists (id ward-<name>, type research, ward-bound) and returns its
// id plus whether THIS call created it (atomic under the single
// writer — concurrent first callers see exactly one created=true) —
// the warden: every ward's selectable bot (ward-v2).
func (s *Store) EnsureWardenSession(ward string) (id string, created bool, err error) {
	id = "ward-" + ward
	res, err := s.db.Exec(`INSERT INTO sessions (id, type, created_at) VALUES (?, 'research', ?)
		ON CONFLICT(id) DO NOTHING`, id, now())
	if err != nil {
		return "", false, fmt.Errorf("store: ensure warden: %w", err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
		created = true
	}
	if err := s.SetSessionWard(id, ward); err != nil {
		return "", false, err
	}
	return id, created, nil
}

// SetSessionTitle names a session (research sessions carry display
// names; chat does not).
func (s *Store) SetSessionTitle(sessionID, title string) error {
	if _, err := s.db.Exec(`UPDATE sessions SET title = NULLIF(?, '') WHERE id = ?`, title, sessionID); err != nil {
		return fmt.Errorf("store: set session title: %w", err)
	}
	return nil
}

// SetSessionWard rebinds a session's sticky ward ("" unbinds). The
// ward tools fire this on ward_use/ward_create; the binding drives the
// sidebar grouping and the next run's pre-loaded ward context.
func (s *Store) SetSessionWard(sessionID, ward string) error {
	if _, err := s.db.Exec(`UPDATE sessions SET ward = NULLIF(?, '') WHERE id = ?`, ward, sessionID); err != nil {
		return fmt.Errorf("store: set session ward: %w", err)
	}
	return nil
}
