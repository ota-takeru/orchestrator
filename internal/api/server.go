package api

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	mux.HandleFunc("/api/decisions", s.handleDecisions)
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
