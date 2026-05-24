package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/storage"
)

type Server struct {
	db        *storage.DB
	projectID string
}

func NewServer(db *storage.DB, projectID string) *Server {
	return &Server{db: db, projectID: projectID}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/ui/snapshot", s.handleUISnapshot)
	mux.HandleFunc("/api/inbox", s.handleInbox)
	mux.HandleFunc("/api/inbox/", s.handleInboxItem)
	mux.HandleFunc("/api/decisions", s.handleDecisions)
	mux.HandleFunc("/api/memory", s.handleMemory)
	mux.HandleFunc("/api/artifacts/trusted", s.handleTrustedArtifacts)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUISnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	snapshot, err := s.db.LoadHumanInboxSnapshot(r.Context(), s.projectID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status := r.URL.Query().Get("status")
	items, err := s.db.ListInboxItems(r.Context(), s.projectID, status)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "inbox_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleInboxItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	id, action, ok := parseInboxActionPath(r.URL.Path)
	if !ok || action != "approve" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown inbox action")
		return
	}
	var input struct {
		Option string `json:"option"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := s.db.ApproveInboxItem(r.Context(), storage.InboxApprovalInput{
		ProjectID: s.projectID,
		InboxID:   id,
		Option:    input.Option,
		Notes:     input.Notes,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "inbox_approve_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status := r.URL.Query().Get("status")
	decisions, err := s.db.ListDecisions(r.Context(), s.projectID, status)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "decisions_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"decisions": decisions})
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	memoryType := r.URL.Query().Get("type")
	memories, err := s.db.ListMemories(r.Context(), s.projectID, memoryType)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"memories": memories})
}

func (s *Server) handleTrustedArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	artifacts, err := s.db.TrustedArtifactContentBundle(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "trusted_artifacts_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}

func parseInboxActionPath(path string) (string, string, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "inbox" {
		return "", "", false
	}
	if strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, code string, message string) {
	writeAPIJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
