package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// ---------- request / response types ----------

// CreateSessionRequest is the JSON body for POST /session/create.
type CreateSessionRequest struct {
	Cwd                   string `json:"cwd"`
	PermissionCallbackURL string `json:"permissionCallbackUrl,omitempty"`
}

// CreateSessionResponse is the JSON response for POST /session/create.
type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
}

// PromptRequest is the JSON body for POST /session/prompt.
type PromptRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

// CloseSessionRequest is the JSON body for POST /session/close.
type CloseSessionRequest struct {
	SessionID string `json:"sessionId"`
}

// ErrorResponse is returned for any error.
type ErrorResponse struct {
	Error string `json:"error"`
}

// OkResponse is returned for successful session close.
type OkResponse struct {
	Ok bool `json:"ok"`
}

// ---------- handler ----------

// Handler holds the HTTP route handlers.
type Handler struct {
	manager *SessionManager
	logger  *slog.Logger
}

// NewHandler creates a new Handler.
func NewHandler(manager *SessionManager, logger *slog.Logger) *Handler {
	return &Handler{manager: manager, logger: logger}
}

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /session/create", h.handleCreateSession)
	mux.HandleFunc("POST /session/prompt", h.handlePrompt)
	mux.HandleFunc("POST /session/close", h.handleCloseSession)
	mux.HandleFunc("GET /health", h.handleHealth)
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}
	if req.Cwd == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "cwd is required"})
		return
	}

	sess, err := h.manager.CreateSession(r.Context(), req.Cwd, req.PermissionCallbackURL)
	if err != nil {
		h.logger.Error("create session failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, CreateSessionResponse{SessionID: sess.ID})
}

func (h *Handler) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var req PromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}
	if req.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "sessionId is required"})
		return
	}
	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "prompt is required"})
		return
	}

	result, err := h.manager.Prompt(r.Context(), req.SessionID, req.Prompt)
	if err != nil {
		h.logger.Error("prompt failed", "sessionId", req.SessionID, "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	var req CloseSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}
	if req.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "sessionId is required"})
		return
	}

	if err := h.manager.CloseSession(req.SessionID); err != nil {
		h.logger.Error("close session failed", "sessionId", req.SessionID, "error", err)
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, OkResponse{Ok: true})
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, OkResponse{Ok: true})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
