package main

// The distiller's session-source adapter (extracted from main.go — LOC
// budget). Reads newest sessions first from the kernel store.

import (
	"context"
	"time"

	"zbot/backend/distiller"
	zbotstore "zbot/backend/internal/store"
)

// storeSource adapts the conversation store to the distiller's
// SessionSource (kernel seam read; newest sessions first).
type storeSource struct{ db *zbotstore.Store }

func (s storeSource) FinishedSessions(ctx context.Context, limit int) ([]distiller.Session, error) {
	sessions, err := s.db.ListSessions(limit)
	if err != nil {
		return nil, err
	}
	var out []distiller.Session
	for _, sess := range sessions {
		msgs, err := s.db.Replay(sess.ID, 0, 500)
		if err != nil || len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		finished := time.Since(last.CreatedAt) >= distiller.IdleThreshold()
		ds := distiller.Session{ID: sess.ID, ExecutionID: last.ExecutionID, Finished: finished}
		for _, m := range msgs {
			ds.Messages = append(ds.Messages, distiller.SessionMessage{Role: string(m.Role), Content: m.Content})
		}
		out = append(out, ds)
	}
	return out, nil
}
