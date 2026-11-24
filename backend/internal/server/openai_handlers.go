package server

// The OpenAI-compatible channel (openai-channel spec): OpenWebUI and
// any OpenAI-shaped client chat with Karta as the backend. Conversations
// map onto sessions — the model name selects the TYPE (karta-research
// runs the full pipeline, karta-chat the lean exchange) and the first
// user message derives a stable session id, so follow-up turns (which
// resend the whole history) continue the same session and appear in
// Karta's sidebar with the full analysis surface.

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zbot/backend/internal/chat"
	apigen "zbot/backend/internal/server/apigen"
)

// openAiSessionID derives the conversation's stable session id: a
// client-supplied metadata.chat_id when present (the PREFERRED
// identity — clients with real conversation ids never collide), else
// the sha1[:16] of the first user message (OpenAI clients resend full
// history; the first message is the fallback identity — two distinct
// conversations with an IDENTICAL opening share a session, a recorded
// protocol-inherent trade-off). The requested model's type suffix
// keeps a hash collision from inheriting the WRONG session type.
func openAiSessionID(req apigen.OpenAiCompletionRequest, typ string) string {
	if req.Metadata != nil && req.Metadata.ChatId != nil && strings.TrimSpace(*req.Metadata.ChatId) != "" {
		id := strings.TrimSpace(*req.Metadata.ChatId)
		if len(id) > 64 {
			sum := sha1.Sum([]byte(id))
			id = hex.EncodeToString(sum[:])[:16]
		}
		return "owui-" + id + "-" + typ
	}
	for _, m := range req.Messages {
		if m.Role == "user" {
			sum := sha1.Sum([]byte(m.Content))
			return "owui-" + hex.EncodeToString(sum[:])[:16] + "-" + typ
		}
	}
	return ""
}

// v1Authorized enforces the optional bearer token (server.api_token).
func (s *Server) v1Authorized(r *http.Request) bool {
	if s.apiToken == "" {
		return true // loopback posture (the request guard covers /v1 too)
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	token := strings.TrimSpace(auth[len(prefix):])
	// Constant-time compare (channel hygiene; loopback makes it moot
	// today, an exposed bind may not).
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) == 1
}

// openAiError emits the OpenAI error shape with an HTTP status.
func openAiError(w http.ResponseWriter, status int, message, errType, code string) {
	var body apigen.OpenAiError
	body.Error.Message = message
	body.Error.Type = errType
	if code != "" {
		c := code
		body.Error.Code = &c
	}
	writeJSON(w, status, body)
}

// ListOpenAiModels advertises the Karta "models" (session types).
func (s *Server) ListOpenAiModels(w http.ResponseWriter, r *http.Request) {
	if !s.v1Authorized(r) {
		openAiError(w, http.StatusUnauthorized, "missing or wrong bearer token", "invalid_request_error", "invalid_api_key")
		return
	}
	writeJSON(w, http.StatusOK, apigen.OpenAiModelList{
		Object: apigen.OpenAiModelListObject("list"),
		Data: []apigen.OpenAiModel{
			{Id: apigen.OpenAiModelId("karta-research"), Object: apigen.OpenAiModelObject("model")},
			{Id: apigen.OpenAiModelId("karta-chat"), Object: apigen.OpenAiModelObject("model")},
		},
	})
}

// OpenAiChatCompletion maps an OpenAI completion onto a Karta session
// send and returns the final assistant message in the OpenAI shape.
func (s *Server) OpenAiChatCompletion(w http.ResponseWriter, r *http.Request) {
	if !s.v1Authorized(r) {
		openAiError(w, http.StatusUnauthorized, "missing or wrong bearer token", "invalid_request_error", "invalid_api_key")
		return
	}
	if s.chat == nil || s.db == nil {
		openAiError(w, http.StatusServiceUnavailable, "chat service not wired", "server_error", "")
		return
	}
	var req apigen.OpenAiCompletionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		openAiError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error", "")
		return
	}
	if len(req.Messages) == 0 {
		openAiError(w, http.StatusBadRequest, "messages must not be empty", "invalid_request_error", "")
		return
	}
	// The model maps to the session TYPE (the id carries it as a
	// suffix, so a hash collision can never inherit the wrong type).
	typ := "research"
	if strings.Contains(string(req.Model), "chat") {
		typ = "chat"
	}
	sessionID := openAiSessionID(req, typ)
	if sessionID == "" {
		openAiError(w, http.StatusBadRequest, "messages must carry at least one user message", "invalid_request_error", "")
		return
	}
	if _, err := s.db.EnsureSession(sessionID, typ); err != nil {
		openAiError(w, http.StatusInternalServerError, "session ensure failed: "+err.Error(), "server_error", "")
		return
	}
	// The LAST user message is this turn's send (history is the
	// client's concern; the transcript is ours). The send routes by the
	// SESSION's type — the model field above decided it.
	var lastUser string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUser = req.Messages[i].Content
			break
		}
	}
	if strings.TrimSpace(lastUser) == "" {
		openAiError(w, http.StatusBadRequest, "the last user message must not be empty", "invalid_request_error", "")
		return
	}
	// The chat service resolves the model from the daemon's configured
	// default (the OpenAI "model" field selected the session TYPE).
	modelRef := ""
	if s.db != nil {
		if m, err := s.db.DefaultModel(); err == nil && m != "" {
			modelRef = m
		}
	}
	providerName, modelID, _ := strings.Cut(modelRef, "/")
	if providerName == "" || modelID == "" {
		openAiError(w, http.StatusBadRequest, "no default model configured — set one in Karta's model picker first", "invalid_request_error", "")
		return
	}
	outcome := s.chat.SendIn(r.Context(), sessionID, lastUser, providerName, modelID)
	if outcome.Err != nil {
		// Typed mapping: busy → OpenAI 409; everything else rides as
		// the exchange's failure message.
		if errors.Is(outcome.Err, chat.ErrSessionBusy) {
			openAiError(w, http.StatusConflict, outcome.Err.Error(), "server_error", "session_busy")
			return
		}
		openAiError(w, http.StatusBadGateway, outcome.Err.Error(), "server_error", "")
		return
	}
	reply := ""
	for i := len(outcome.Rows) - 1; i >= 0; i-- {
		if outcome.Rows[i].Role == "assistant" && outcome.Rows[i].Content != "" {
			reply = outcome.Rows[i].Content
			break
		}
	}
	if reply == "" {
		openAiError(w, http.StatusBadGateway, "the exchange produced no assistant message", "server_error", "")
		return
	}
	resp := apigen.OpenAiCompletionResponse{
		Id:      fmt.Sprintf("chatcmpl-karta-%d", time.Now().UnixNano()),
		Object:  apigen.OpenAiCompletionResponseObject("chat.completion"),
		Created: int(time.Now().Unix()),
		Model:   string(req.Model),
	}
	var choice struct {
		FinishReason apigen.OpenAiCompletionResponseChoicesFinishReason `json:"finish_reason"`
		Index        int                                                `json:"index"`
		Message      struct {
			Content string                                            `json:"content"`
			Role    apigen.OpenAiCompletionResponseChoicesMessageRole `json:"role"`
		} `json:"message"`
	}
	choice.Index = 0
	choice.FinishReason = apigen.OpenAiCompletionResponseChoicesFinishReason("stop")
	choice.Message.Content = reply
	choice.Message.Role = apigen.OpenAiCompletionResponseChoicesMessageRole("assistant")
	resp.Choices = append(resp.Choices, choice)
	writeJSON(w, http.StatusOK, resp)
}
