package server

import (
	"errors"
	"net/http"
	"strings"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
	"zbot/backend/internal/store"
)

// SyncChatMessages pages the eternal transcript. next_cursor is the last
// DELIVERED seq (a page cursor, never the database tail).
func (s *Server) SyncChatMessages(w http.ResponseWriter, r *http.Request, params apigen.SyncChatMessagesParams) {
	afterSeq := int64(0)
	if params.AfterSeq != nil {
		afterSeq = int64(*params.AfterSeq)
	}
	limit := 100
	if params.Limit != nil {
		limit = int(*params.Limit)
	}
	if afterSeq < 0 || limit < 1 || limit > 500 {
		writeError(w, http.StatusBadRequest, "invalid_request", "after_seq must be >= 0; limit must be 1-500")
		return
	}
	rows, next, truncated, err := s.chat.Sync(afterSeq, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]apigen.ChatMessage, 0, len(rows))
	for _, m := range rows {
		out = append(out, chatView(m))
	}
	writeJSON(w, http.StatusOK, apigen.ChatSync{
		Messages: out, NextCursor: int(next), Truncated: truncated,
	})
}

// SendChatMessage runs one serialized exchange and returns every row it
// appended (failures return the persisted user row + cursor).
func (s *Server) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	var req apigen.ChatSend
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "text must not be empty")
		return
	}
	providerName, modelID, found := strings.Cut(req.Model, "/")
	if !found || providerName == "" || modelID == "" {
		writeError(w, http.StatusBadRequest, "model_required", "select a model in the header picker first")
		return
	}

	outcome := s.chat.Send(r.Context(), req.Text, providerName, modelID)
	// Persist the model as the default (last-used wins).
	if s.db != nil {
		_ = s.db.SetDefaultModel(req.Model)
	}
	if outcome.Err != nil {
		body := apigen.ChatExchange{
			Messages:  chatViews(outcome.Rows),
			EndCursor: int(outcome.EndCursor),
			Error:     chatErrorView(outcome.Err),
		}
		writeJSON(w, http.StatusBadGateway, body)
		return
	}
	writeJSON(w, http.StatusOK, apigen.ChatExchange{
		Messages: chatViews(outcome.Rows), EndCursor: int(outcome.EndCursor),
	})
}

func chatViews(rows []store.Message) []apigen.ChatMessage {
	out := make([]apigen.ChatMessage, 0, len(rows))
	for _, m := range rows {
		out = append(out, chatView(m))
	}
	return out
}

func chatView(m store.Message) apigen.ChatMessage {
	view := apigen.ChatMessage{
		Id: m.ID, Seq: int(m.Seq), Role: apigen.ChatMessageRole(m.Role),
		Content: m.Content, CreatedAt: m.CreatedAt,
		ToolCallId: nilStrPtr(m.ToolCallID), ToolName: nilStrPtr(m.ToolName),
		Provider: nilStrPtr(m.Provider), Model: nilStrPtr(m.Model),
	}
	if m.ToolCalls != "" {
		view.ToolCalls = &m.ToolCalls
	}
	if m.PromptTokens.Valid {
		v := int(m.PromptTokens.Int64)
		view.PromptTokens = &v
	}
	if m.CompletionTokens.Valid {
		v := int(m.CompletionTokens.Int64)
		view.CompletionTokens = &v
	}
	return view
}

func chatErrorView(err error) *apigen.ProviderError {
	reason := apigen.ProviderFailureReason(providers.ReasonOf(err))
	if reason == "" {
		if errors.Is(err, providers.ErrKeyringUnavailable) {
			reason = apigen.ProviderFailureReason("credential_unavailable")
		} else {
			reason = apigen.ProviderFailureReason("unreachable")
		}
	}
	return &apigen.ProviderError{Code: "provider_failure", Message: err.Error(), Reason: reason}
}

func nilStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var _ = chat.SessionID

// GetDefaultModel returns the stored default model for the chat.
func (s *Server) GetDefaultModel(w http.ResponseWriter, r *http.Request) {
	model, err := s.db.DefaultModel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

// ClearChat wipes the eternal conversation's transcript (user-initiated,
// destructive).
func (s *Server) ClearChat(w http.ResponseWriter, r *http.Request) {
	if err := s.db.ClearSession(chat.SessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
