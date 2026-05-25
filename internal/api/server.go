package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/projecthub"
	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/storage"
)

type Server struct {
	db         *storage.DB
	projectID  string
	hub        *projecthub.Hub
	localToken string
}

func NewServer(db *storage.DB, projectID string) *Server {
	return &Server{db: db, projectID: projectID}
}

func NewServerWithHub(db *storage.DB, projectID string, hub *projecthub.Hub) *Server {
	return &Server{db: db, projectID: projectID, hub: hub}
}

func (s *Server) WithLocalToken(token string) *Server {
	s.localToken = strings.TrimSpace(token)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/projects", s.handleProjects)
	mux.HandleFunc("/api/projects/", s.handleProjectRoute)
	mux.HandleFunc("/api/ui/snapshot", s.handleUISnapshot)
	mux.HandleFunc("/api/inbox", s.handleInbox)
	mux.HandleFunc("/api/inbox/", s.handleInboxItem)
	mux.HandleFunc("/api/decisions", s.handleDecisions)
	mux.HandleFunc("/api/memory", s.handleMemory)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/tasks/", s.handleTaskRoute)
	mux.HandleFunc("/api/requests", s.handleRequests)
	mux.HandleFunc("/api/queue", s.handleQueue)
	mux.HandleFunc("/api/work/status", s.handleWorkStatus)
	mux.HandleFunc("/api/planning/status", s.handlePlanningStatus)
	mux.HandleFunc("/api/change-requests", s.handleChangeRequests)
	mux.HandleFunc("/api/dependency-risks", s.handleDependencyRisks)
	mux.HandleFunc("/api/env/bindings", s.handleEnvBindings)
	mux.HandleFunc("/api/artifacts/trusted", s.handleTrustedArtifacts)
	mux.HandleFunc("/api/platform/path-mappings", s.handlePathMappings)
	mux.HandleFunc("/api/platform/toolchain-setup", s.handleToolchainSetup)
	mux.HandleFunc("/api/merge/status", s.handleMergeStatus)
	mux.HandleFunc("/api/check", s.handleProjectCheck)
	mux.HandleFunc("/api/setup", s.handleSetupStatus)
	return s.localMiddleware(mux)
}

func (s *Server) localMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-DevOS-Token, X-DevOS-Nonce")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && !isLocalhostOrigin(origin) {
				writeAPIError(w, http.StatusForbidden, "cors_forbidden", "only localhost origins are allowed")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if s.localToken != "" && requiresLocalToken(r) && r.Header.Get("X-DevOS-Token") != s.localToken {
			writeAPIError(w, http.StatusUnauthorized, "local_token_required", "X-DevOS-Token is required")
			return
		}
		if s.localToken != "" && requiresLocalNonce(r) && strings.TrimSpace(r.Header.Get("X-DevOS-Nonce")) == "" {
			writeAPIError(w, http.StatusForbidden, "local_nonce_required", "X-DevOS-Nonce is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalhostOrigin(origin string) bool {
	u, err := urlParse(origin)
	if err != nil {
		return false
	}
	host := u
	if parsedHost, _, err := net.SplitHostPort(u); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func requiresLocalToken(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodOptions {
		return false
	}
	return r.URL.Path == "/api/env/bindings" ||
		strings.HasSuffix(r.URL.Path, "/env/bindings") ||
		strings.Contains(r.URL.Path, "/approve") ||
		strings.Contains(r.URL.Path, "/merge") ||
		strings.Contains(r.URL.Path, "/review/") ||
		strings.HasSuffix(r.URL.Path, "/verify")
}

func requiresLocalNonce(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodOptions {
		return false
	}
	return strings.Contains(r.URL.Path, "/approve") || strings.Contains(r.URL.Path, "/merge")
}

func urlParse(origin string) (string, error) {
	withoutScheme := origin
	if idx := strings.Index(withoutScheme, "://"); idx >= 0 {
		withoutScheme = withoutScheme[idx+3:]
	}
	if withoutScheme == "" || strings.Contains(withoutScheme, "/") {
		return "", errors.New("invalid origin")
	}
	return withoutScheme, nil
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	tasks, err := s.db.ListTasks(r.Context(), s.projectID, r.URL.Query().Get("status"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "tasks_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleTaskRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "tasks" || strings.TrimSpace(parts[2]) == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown task route")
		return
	}
	taskID := parts[2]
	switch {
	case len(parts) == 4 && parts[3] == "artifacts":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		artifacts, err := s.db.ListTaskRunArtifacts(r.Context(), s.projectID, taskID, true)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "task_artifacts_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
	case len(parts) == 4 && parts[3] == "verify":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		result, err := s.db.VerifyTask(r.Context(), s.projectID, taskID, storage.VerifyTaskInput{Adapter: "local"})
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "task_verify_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, result)
	case len(parts) == 5 && parts[3] == "review" && parts[4] == "approve":
		s.handleTaskApproval(w, r, taskID, storage.ApprovalFinalReview, false)
	case len(parts) == 5 && parts[3] == "review" && parts[4] == "reject":
		s.handleTaskApproval(w, r, taskID, storage.ApprovalFinalReview, true)
	case len(parts) == 5 && parts[3] == "merge" && parts[4] == "approve":
		s.handleTaskApproval(w, r, taskID, storage.ApprovalMerge, false)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown task route")
	}
}

func (s *Server) handleTaskApproval(w http.ResponseWriter, r *http.Request, taskID string, approvalType storage.ApprovalType, reject bool) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	notes, ok := decodeNotesBody(w, r)
	if !ok {
		return
	}
	var (
		result storage.ApprovalRecord
		err    error
	)
	if reject {
		result, err = s.db.RejectTaskFinalReview(r.Context(), storage.ApprovalInput{ProjectID: s.projectID, TaskID: taskID, Notes: notes})
	} else {
		result, err = s.db.ApproveTaskEvidence(r.Context(), storage.ApprovalInput{ProjectID: s.projectID, TaskID: taskID, ApprovalType: approvalType, Notes: notes})
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "task_approval_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/projects" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown projects route")
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	if s.hub == nil || s.hub.Registry == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "project_registry_unavailable", "project registry is not configured")
		return
	}
	projects, err := s.hub.Registry.ListProjects(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "projects_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleProjectRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "projects" || strings.TrimSpace(parts[2]) == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown project route")
		return
	}
	projectID := parts[2]
	action := parts[3]
	project, authority, err := s.projectAuthority(r.Context(), projectID)
	if err != nil {
		writeProjectHubError(w, err)
		return
	}
	switch {
	case len(parts) == 4 && action == "snapshot":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		snapshot, err := authority.Snapshot(r.Context(), project)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, snapshot)
	case len(parts) == 4 && action == "dashboard":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.Dashboard(r.Context(), project)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "tasks":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.Tasks(r.Context(), project)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 6 && action == "tasks" && parts[4] != "" && parts[5] == "artifacts":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.TaskArtifacts(r.Context(), project, parts[4])
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 6 && action == "tasks" && parts[4] != "" && parts[5] == "verify":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		body, err := authority.VerifyTask(r.Context(), project, parts[4])
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 7 && action == "tasks" && parts[4] != "" && parts[5] == "review" && parts[6] == "approve":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		notes, ok := decodeNotesBody(w, r)
		if !ok {
			return
		}
		body, err := authority.ApproveTaskReview(r.Context(), project, parts[4], notes)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 7 && action == "tasks" && parts[4] != "" && parts[5] == "review" && parts[6] == "reject":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		notes, ok := decodeNotesBody(w, r)
		if !ok {
			return
		}
		body, err := authority.RejectTaskReview(r.Context(), project, parts[4], notes)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 7 && action == "tasks" && parts[4] != "" && parts[5] == "merge" && parts[6] == "approve":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		notes, ok := decodeNotesBody(w, r)
		if !ok {
			return
		}
		body, err := authority.ApproveTaskMerge(r.Context(), project, parts[4], notes)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "inbox":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.Inbox(r.Context(), project, r.URL.Query().Get("status"))
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "requests":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		text, ok := decodeTextBody(w, r)
		if !ok {
			return
		}
		body, err := authority.CreateFeatureRequest(r.Context(), project, text)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "change-requests":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		text, ok := decodeTextBody(w, r)
		if !ok {
			return
		}
		body, err := authority.CreateChangeRequest(r.Context(), project, text)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 5 && action == "env" && parts[4] == "bindings":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		input, ok := decodeEnvBindingBody(w, r)
		if !ok {
			return
		}
		body, err := authority.SaveEnvBinding(r.Context(), project, input)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "setup":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.SetupStatus(r.Context(), project)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "refresh":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		updated, snapshot, err := s.hub.Refresh(r.Context(), projectID)
		if err != nil {
			writeAPIJSON(w, http.StatusBadGateway, map[string]any{
				"project": updated,
				"error": map[string]string{
					"code":    "project_refresh_failed",
					"message": err.Error(),
				},
			})
			return
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"project": updated, "snapshot": snapshot})
	case len(parts) == 6 && action == "inbox" && parts[5] == "approve":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
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
		body, err := authority.ApproveInboxItem(r.Context(), project, parts[4], input.Option, input.Notes)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown project route")
	}
}

func (s *Server) projectAuthority(ctx context.Context, projectID string) (registry.RegisteredProject, projecthub.ProjectAuthority, error) {
	if s.hub == nil || s.hub.Registry == nil {
		return registry.RegisteredProject{}, nil, errors.New("project registry is not configured")
	}
	return s.hub.Project(ctx, projectID)
}

func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		requests, err := s.db.ListFeatureRequests(r.Context(), s.projectID, r.URL.Query().Get("status"))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "requests_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"requests": requests})
	case http.MethodPost:
		var input struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		result, err := s.db.CreateFeatureRequest(r.Context(), s.projectID, input.Text)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "request_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, result)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST is required")
	}
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	items, err := s.db.ListWorkQueueItems(r.Context(), s.projectID, r.URL.Query().Get("status"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "queue_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleWorkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status, err := s.db.GetWorkStatus(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "work_status_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, status)
}

func (s *Server) handlePlanningStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status, err := s.db.GetPlanningStatus(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "planning_status_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, status)
}

func (s *Server) handleChangeRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		requests, err := s.db.ListChangeRequests(r.Context(), s.projectID, r.URL.Query().Get("status"))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "change_requests_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"change_requests": requests})
	case http.MethodPost:
		var input struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		result, err := s.db.CreateChangeRequest(r.Context(), s.projectID, input.Text)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "change_request_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, result)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST is required")
	}
}

func (s *Server) handleDependencyRisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	records, err := s.db.ListDependencyRisks(r.Context(), storage.DependencyRiskListFilter{
		ProjectID:      s.projectID,
		PackageManager: r.URL.Query().Get("manager"),
		DependencyType: r.URL.Query().Get("type"),
		Risk:           r.URL.Query().Get("risk"),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "dependency_risks_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"risks": records})
}

func (s *Server) handleEnvBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	input, ok := decodeEnvBindingBody(w, r)
	if !ok {
		return
	}
	input.ProjectID = s.projectID
	record, err := s.db.SaveEnvBinding(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "env_binding_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, record)
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

func (s *Server) handlePathMappings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	mappings, err := s.db.ListPathMappings(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "path_mappings_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"mappings": mappings})
}

func (s *Server) handleToolchainSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	cards, err := s.db.ListToolchainSetupCards(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "toolchain_setup_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

func (s *Server) handleMergeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status, err := s.db.MergeGateStatus(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "merge_status_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, status)
}

func (s *Server) handleProjectCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	violations, err := s.db.CheckProjectInvariants(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "check_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"violations": violations})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	status, err := s.db.LoadSetupStatus(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "setup_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, status)
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

func decodeTextBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return "", false
	}
	return input.Text, true
}

func decodeNotesBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Body == nil {
		return "", true
	}
	var input struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return "", false
	}
	return input.Notes, true
}

func decodeEnvBindingBody(w http.ResponseWriter, r *http.Request) (storage.EnvBindingInput, bool) {
	var input struct {
		EnvironmentID string `json:"environment_id"`
		Key           string `json:"key"`
		Scope         string `json:"scope"`
		ScopeID       string `json:"scope_id"`
		Value         string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return storage.EnvBindingInput{}, false
	}
	return storage.EnvBindingInput{
		EnvironmentID: input.EnvironmentID,
		Key:           input.Key,
		Scope:         input.Scope,
		ScopeID:       input.ScopeID,
		Value:         input.Value,
	}, true
}

func writeProjectHubError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, registry.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "project_not_found", err.Error())
		return
	}
	var authorityErr *projecthub.AuthorityError
	if errors.As(err, &authorityErr) {
		status := http.StatusBadGateway
		if authorityErr.Code == "wsl_project_invalid" {
			status = http.StatusBadRequest
		}
		writeAPIJSON(w, status, map[string]any{"error": authorityErr})
		return
	}
	if strings.Contains(err.Error(), "invalid authority_runtime") {
		writeAPIError(w, http.StatusBadRequest, "invalid_authority_runtime", err.Error())
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "project_authority_failed", err.Error())
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
