package server

import (
	"net/http"

	apigen "zbot/backend/internal/server/apigen"
)

// ContinueSession grants additional iterations to a live research run
// (the progress tracker's user-side extension). Only valid while the
// run is active; a finished session returns 409.
func (s *Server) ContinueSession(w http.ResponseWriter, r *http.Request, sessionId string) {
	additional := 25
	if r.ContentLength > 0 {
		var req apigen.ContinueSessionJSONBody
		if err := decodeJSON(w, r, &req); err != nil {
			return
		}
		if req.AdditionalIterations != nil {
			additional = *req.AdditionalIterations
		}
	}
	if additional < 1 || additional > 200 {
		writeError(w, http.StatusBadRequest, "invalid_request", "additional_iterations must be 1-200")
		return
	}
	// The engine adapter owns the per-session extend closures; the
	// server routes through the seam.
	if s.engineExtender == nil {
		writeError(w, http.StatusConflict, "not_active", "no active run to extend")
		return
	}
	granted, err := s.engineExtender(sessionId, additional)
	if err != nil {
		writeError(w, http.StatusConflict, "not_active", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Granted int `json:"granted"`
	}{Granted: granted})
}

// SessionExtender grants iterations to a live run (the daemon wires the
// adapter's per-session closure). Returns the granted amount or a typed
// error (no active run).
type SessionExtender func(sessionID string, additional int) (int, error)
