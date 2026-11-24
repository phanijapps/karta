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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zbot/backend/internal/chat"
	apigen "zbot/backend/internal/server/apigen"
)

// openAiSessionID derives the conversation's stable session id from its
// first user message (OpenAI clients resend full history; the first
// message is the conversation's identity).
func openAiSessionID(req apigen.OpenAiCompletionRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			sum := sha1.Sum([]byte(m.Content))
			return "owui-" + hex.EncodeToString(sum[:])[:16]
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
	if err := decodeJSON(w, r, &req); err != nil {
		return // decodeJSON wrote the typed 400
	}
	sessionID := openAiSessionID(req)
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "messages must carry at least one user message")
		return
	}
	// The model maps to the session TYPE: ensure the row exists with
	// that type (idempotent; the type is immutable at creation).
	typ := "research"
	if strings.Contains(string(req.Model), "chat") {
		typ = "chat"
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
		writeError(w, http.StatusBadRequest, "invalid_request", "the last user message must not be empty")
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
