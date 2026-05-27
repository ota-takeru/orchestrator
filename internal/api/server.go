package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/artifactgen"
	"github.com/ota-takeru/orchestrator/internal/preflight"
	"github.com/ota-takeru/orchestrator/internal/projecthub"
	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/storage"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
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
	mux.HandleFunc("/api/tasks/materialize", s.handleTasksMaterialize)
	mux.HandleFunc("/api/tasks/", s.handleTaskRoute)
	mux.HandleFunc("/api/requests", s.handleRequests)
	mux.HandleFunc("/api/queue", s.handleQueue)
	mux.HandleFunc("/api/work/start", s.handleWorkStart)
	mux.HandleFunc("/api/work/status", s.handleWorkStatus)
	mux.HandleFunc("/api/planning/status", s.handlePlanningStatus)
	mux.HandleFunc("/api/change-requests", s.handleChangeRequests)
	mux.HandleFunc("/api/change-requests/", s.handleChangeRequestRoute)
	mux.HandleFunc("/api/dependency-risks", s.handleDependencyRisks)
	mux.HandleFunc("/api/dependency-approvals", s.handleDependencyApprovals)
	mux.HandleFunc("/api/env/bindings", s.handleEnvBindings)
	mux.HandleFunc("/api/artifacts", s.handleArtifacts)
	mux.HandleFunc("/api/artifacts/", s.handleArtifactRoute)
	mux.HandleFunc("/api/artifacts/trusted", s.handleTrustedArtifacts)
	mux.HandleFunc("/api/platform/path-mappings", s.handlePathMappings)
	mux.HandleFunc("/api/platform/toolchain-setup", s.handleToolchainSetup)
	mux.HandleFunc("/api/merge/status", s.handleMergeStatus)
	mux.HandleFunc("/api/check", s.handleProjectCheck)
	mux.HandleFunc("/api/setup", s.handleSetupStatus)
	mux.HandleFunc("/api/setup/actions/", s.handleSetupAction)
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
		r.URL.Path == "/api/projects" ||
		strings.HasSuffix(r.URL.Path, "/env/bindings") ||
		strings.Contains(r.URL.Path, "/approve") ||
		strings.HasSuffix(r.URL.Path, "/tasks/materialize") ||
		strings.Contains(r.URL.Path, "/change-requests/") ||
		strings.Contains(r.URL.Path, "/work/start") ||
		strings.Contains(r.URL.Path, "/merge") ||
		strings.Contains(r.URL.Path, "/review/") ||
		strings.HasSuffix(r.URL.Path, "/verify") ||
		strings.Contains(r.URL.Path, "/dependency-approvals") ||
		strings.Contains(r.URL.Path, "/setup/actions/")
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

func (s *Server) handleTasksMaterialize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	tasks, err := s.db.MaterializeApprovedTasks(r.Context(), s.projectID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "tasks_materialize_failed", err.Error())
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
	if s.hub == nil || s.hub.Registry == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "project_registry_unavailable", "project registry is not configured")
		return
	}
	if r.Method == http.MethodPost {
		s.handleProjectCreate(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST is required")
		return
	}
	projects, err := s.hub.Registry.ListProjects(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "projects_failed", err.Error())
		return
	}
	current := s.currentProjectSummary(r.Context())
	if current != nil {
		for _, project := range projects {
			if filepath.Clean(project.ProjectRoot) == filepath.Clean(current.ProjectRoot) {
				current.Registered = true
				current = nil
				break
			}
		}
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"projects":        projects,
		"current_project": current,
		"runtime_options": detectRuntimeOptions(),
	})
}

type projectCreateRequest struct {
	DisplayName              string                    `json:"display_name"`
	ProjectRoot              string                    `json:"project_root"`
	Concept                  string                    `json:"concept"`
	AuthorityRuntime         registry.AuthorityRuntime `json:"authority_runtime"`
	WSLDistro                string                    `json:"wsl_distro"`
	WindowsDisplayRoot       string                    `json:"windows_display_root"`
	GenerateInitialArtifacts bool                      `json:"generate_initial_artifacts"`
}

type projectRuntimeOption struct {
	AuthorityRuntime registry.AuthorityRuntime `json:"authority_runtime"`
	Label            string                    `json:"label"`
	Description      string                    `json:"description"`
	Detected         bool                      `json:"detected"`
	Available        bool                      `json:"available"`
	Recommended      bool                      `json:"recommended"`
	WSLDistro        string                    `json:"wsl_distro,omitempty"`
}

type currentProjectSummary struct {
	ID                   string                    `json:"id"`
	DisplayName          string                    `json:"display_name"`
	AuthorityRuntime     registry.AuthorityRuntime `json:"authority_runtime"`
	PrimaryEnvironmentID string                    `json:"primary_environment_id"`
	ProjectRoot          string                    `json:"project_root"`
	Status               string                    `json:"status"`
	Registered           bool                      `json:"registered"`
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	var input projectCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := s.createProject(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "project_create_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusCreated, result)
}

func (s *Server) createProject(ctx context.Context, input projectCreateRequest) (map[string]any, error) {
	name := strings.TrimSpace(input.DisplayName)
	if name == "" {
		return nil, fmt.Errorf("project name is required")
	}
	concept := strings.TrimSpace(input.Concept)
	if concept == "" {
		return nil, fmt.Errorf("concept is required")
	}
	root, err := prepareProjectRoot(ctx, input.ProjectRoot)
	if err != nil {
		return nil, err
	}
	option, err := selectedRuntimeOption(input)
	if err != nil {
		return nil, err
	}
	if err := ensureGitignoreDefaults(root); err != nil {
		return nil, err
	}
	initResult, err := preflight.InitProject(ctx, root, concept)
	if err != nil {
		return nil, err
	}
	toolchainReport := toolchains.RunDoctor(ctx, initResult.PreflightReport.Environment, toolchains.Options{IncludeCodex: false})
	db, dbPath, err := openProjectDataDB(ctx, initResult.ProjectRoot, "")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	projectRecord, err := db.SaveProjectInit(ctx, storage.ProjectInitInput{
		Name:            name,
		RootPath:        initResult.ProjectRoot,
		Environment:     initResult.PreflightReport.Environment,
		PreflightReport: initResult.PreflightReport,
		ToolchainReport: &toolchainReport,
	})
	if err != nil {
		return nil, err
	}
	if err := db.SaveToolchainReport(ctx, projectRecord.ID, toolchainReport); err != nil {
		return nil, err
	}
	artifacts := []storage.ArtifactVersionRecord{}
	if input.GenerateInitialArtifacts {
		artifacts, err = saveGeneratedArtifacts(ctx, db, projectRecord.ID, initResult.ProjectRoot, artifactgen.BuildInitialArtifacts(initResult.ProjectRoot, concept, true))
		if err != nil {
			return nil, err
		}
	}
	add := registry.AddProjectInput{
		DisplayName:      name,
		AuthorityRuntime: option.AuthorityRuntime,
		DataRoot:         filepath.Dir(dbPath),
	}
	switch option.AuthorityRuntime {
	case registry.AuthorityWindows:
		add.ProjectRoot = initResult.ProjectRoot
	case registry.AuthorityWSL:
		add.WSLDistro = option.WSLDistro
		add.WSLProjectRoot = initResult.ProjectRoot
		add.WindowsDisplayRoot = strings.TrimSpace(input.WindowsDisplayRoot)
	default:
		return nil, fmt.Errorf("unsupported authority runtime: %s", option.AuthorityRuntime)
	}
	project, err := s.hub.Registry.AddProject(ctx, add)
	if err != nil {
		return nil, err
	}
	dashboard, err := db.LoadProjectDashboard(ctx, projectRecord.ID, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"project":          project,
		"project_record":   projectRecord,
		"database_path":    dbPath,
		"init_result":      initResult,
		"toolchain_report": toolchainReport,
		"artifacts":        artifacts,
		"dashboard":        dashboard,
	}, nil
}

func openProjectDataDB(ctx context.Context, projectRoot string, dataRoot string) (*storage.DB, string, error) {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = filepath.Join(projectRoot, "orchestrator-data")
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return nil, "", err
	}
	dbPath := filepath.Join(dataRoot, "devos.sqlite")
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		return nil, "", err
	}
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		_ = db.Close()
		return nil, "", err
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	return db, dbPath, nil
}

func prepareProjectRoot(ctx context.Context, projectRoot string) (string, error) {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return "", fmt.Errorf("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("project root is not a directory: %s", abs)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "git", "-C", abs, "init")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git init failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
	} else if err != nil {
		return "", err
	}
	config := exec.CommandContext(ctx, "git", "-C", abs, "config", "core.autocrlf", "false")
	if out, err := config.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git config core.autocrlf failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return abs, nil
}

func ensureGitignoreDefaults(root string) error {
	path := filepath.Join(root, ".gitignore")
	existing := ""
	if raw, err := os.ReadFile(path); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return err
	}
	required := []string{".env.local", ".env.*", "orchestrator-data/", ".devagent-worktrees/"}
	var additions []string
	for _, entry := range required {
		if !gitignoreContains(existing, entry) {
			additions = append(additions, entry)
		}
	}
	if len(additions) == 0 {
		return ensureGitattributesDefault(root)
	}
	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	for _, entry := range additions {
		b.WriteString(entry)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return ensureGitattributesDefault(root)
}

func gitignoreContains(content string, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

func ensureGitattributesDefault(root string) error {
	path := filepath.Join(root, ".gitattributes")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte("* text=auto eol=lf\n"), 0o644)
}

func saveGeneratedArtifacts(ctx context.Context, db *storage.DB, projectID string, root string, artifacts []artifactgen.Artifact) ([]storage.ArtifactVersionRecord, error) {
	records := make([]storage.ArtifactVersionRecord, 0, len(artifacts))
	for _, artifact := range artifacts {
		absPath := filepath.Join(root, filepath.FromSlash(artifact.Path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(absPath, artifact.Content, 0o644); err != nil {
			return nil, err
		}
		record, err := db.SaveArtifactVersion(ctx, storage.ArtifactVersionInput{
			ProjectID:    projectID,
			ArtifactType: artifact.Type,
			Path:         filepath.ToSlash(artifact.Path),
			Content:      artifact.Content,
			Status:       "proposed",
		})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func selectedRuntimeOption(input projectCreateRequest) (projectRuntimeOption, error) {
	selected := input.AuthorityRuntime
	if strings.TrimSpace(string(selected)) == "" {
		for _, option := range detectRuntimeOptions() {
			if option.Recommended && option.Available {
				return option, nil
			}
		}
		return projectRuntimeOption{}, fmt.Errorf("authority runtime is required")
	}
	for _, option := range detectRuntimeOptions() {
		if option.AuthorityRuntime == selected && option.Available {
			if option.AuthorityRuntime == registry.AuthorityWSL && strings.TrimSpace(input.WSLDistro) != "" {
				option.WSLDistro = strings.TrimSpace(input.WSLDistro)
			}
			return option, nil
		}
	}
	return projectRuntimeOption{}, fmt.Errorf("authority runtime %s is not available from this server", selected)
}

func detectRuntimeOptions() []projectRuntimeOption {
	if runtime.GOOS == "windows" {
		return []projectRuntimeOption{{
			AuthorityRuntime: registry.AuthorityWindows,
			Label:            "Windows",
			Description:      "Create and operate the project from this Windows host.",
			Detected:         true,
			Available:        true,
			Recommended:      true,
		}}
	}
	if distro := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")); distro != "" {
		return []projectRuntimeOption{{
			AuthorityRuntime: registry.AuthorityWSL,
			Label:            "WSL",
			Description:      "Create and operate the project inside the current WSL distribution.",
			Detected:         true,
			Available:        true,
			Recommended:      true,
			WSLDistro:        distro,
		}}
	}
	return []projectRuntimeOption{{
		AuthorityRuntime: registry.AuthorityWSL,
		Label:            "WSL",
		Description:      "WSL is not detected for this server process.",
		Detected:         false,
		Available:        false,
	}}
}

func (s *Server) currentProjectSummary(ctx context.Context) *currentProjectSummary {
	if s == nil || s.db == nil || strings.TrimSpace(s.projectID) == "" {
		return nil
	}
	setup, err := s.db.LoadSetupStatus(ctx, s.projectID)
	if err != nil {
		return nil
	}
	authority := registry.AuthorityWindows
	primary := "windows-main"
	if strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" {
		authority = registry.AuthorityWSL
		primary = "wsl-main"
	}
	return &currentProjectSummary{
		ID:                   s.projectID,
		DisplayName:          filepath.Base(setup.ProjectRoot),
		AuthorityRuntime:     authority,
		PrimaryEnvironmentID: primary,
		ProjectRoot:          setup.ProjectRoot,
		Status:               "active",
		Registered:           false,
	}
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
	case len(parts) == 6 && action == "change-requests" && parts[5] == "analyze":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		body, err := authority.AnalyzeChangeRequest(r.Context(), project, parts[4])
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 6 && action == "change-requests" && parts[5] == "approve":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		option, ok := decodeOptionBody(w, r, "approve")
		if !ok {
			return
		}
		body, err := authority.ApproveChangeRequest(r.Context(), project, parts[4], option)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 5 && action == "work" && parts[4] == "start":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		input, ok := decodeWorkStartBody(w, r)
		if !ok {
			return
		}
		body, err := authority.StartWork(r.Context(), project, input)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "artifacts":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		body, err := authority.Artifacts(r.Context(), project, r.URL.Query().Get("type"))
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 6 && action == "artifacts" && parts[5] == "approve":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		input, ok := decodeArtifactApprovalBody(w, r)
		if !ok {
			return
		}
		body, err := authority.ApproveArtifact(r.Context(), project, parts[4], input.Version, input.Status, input.Notes)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 5 && action == "tasks" && parts[4] == "materialize":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		body, err := authority.MaterializeTasks(r.Context(), project)
		if err != nil {
			writeProjectHubError(w, err)
			return
		}
		writeAPIJSON(w, http.StatusOK, body)
	case len(parts) == 4 && action == "dependency-approvals":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		input, ok := decodeDependencyApprovalBody(w, r)
		if !ok {
			return
		}
		body, err := authority.RequestDependencyApproval(r.Context(), project, input)
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
	case len(parts) == 6 && action == "setup" && parts[4] == "actions" && strings.TrimSpace(parts[5]) != "":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		body, err := authority.SetupAction(r.Context(), project, parts[5])
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

func (s *Server) handleWorkStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	input, ok := decodeWorkStartBody(w, r)
	if !ok {
		return
	}
	input.ProjectID = s.projectID
	result, err := s.db.StartWork(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "work_start_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
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

func (s *Server) handleChangeRequestRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "change-requests" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown change request route")
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	switch parts[3] {
	case "analyze":
		result, err := s.db.AnalyzeChangeRequest(r.Context(), s.projectID, parts[2])
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "change_analyze_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, result)
	case "approve":
		option, ok := decodeOptionBody(w, r, "approve")
		if !ok {
			return
		}
		record, err := s.db.ApproveChangeRequest(r.Context(), storage.ChangeApproveInput{ProjectID: s.projectID, ChangeRequestID: parts[2], Option: option})
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "change_approve_failed", err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, record)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown change request action")
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

func (s *Server) handleDependencyApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	input, ok := decodeDependencyApprovalBody(w, r)
	if !ok {
		return
	}
	input.ProjectID = s.projectID
	result, err := s.db.RequestDependencyApproval(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "dependency_approval_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
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

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	artifacts, err := s.db.ListArtifacts(r.Context(), s.projectID, r.URL.Query().Get("type"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "artifacts_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}

func (s *Server) handleArtifactRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "artifacts" || parts[3] != "approve" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown artifact route")
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	input, ok := decodeArtifactApprovalBody(w, r)
	if !ok {
		return
	}
	record, err := s.db.ApproveArtifactVersion(r.Context(), s.projectID, parts[2], input.Version, input.Status, input.Notes)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "artifact_approve_failed", err.Error())
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

func (s *Server) handleSetupAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "setup" || parts[2] != "actions" || strings.TrimSpace(parts[3]) == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown setup action route")
		return
	}
	result, err := s.db.RunSetupAction(r.Context(), s.projectID, parts[3])
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "setup_action_failed", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
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

func decodeOptionBody(w http.ResponseWriter, r *http.Request, fallback string) (string, bool) {
	var input struct {
		Option string `json:"option"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return "", false
		}
	}
	if strings.TrimSpace(input.Option) == "" {
		input.Option = fallback
	}
	return input.Option, true
}

func decodeWorkStartBody(w http.ResponseWriter, r *http.Request) (storage.WorkStartInput, bool) {
	var input struct {
		Mode                      string `json:"mode"`
		Adapter                   string `json:"adapter"`
		PlanningConcurrency       int    `json:"planning_concurrency"`
		ImplementationConcurrency int    `json:"implementation_concurrency"`
		Until                     string `json:"until"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return storage.WorkStartInput{}, false
		}
	}
	if input.Mode == "" {
		input.Mode = "sequential"
	}
	if input.Adapter == "" {
		input.Adapter = "fake"
	}
	if input.PlanningConcurrency == 0 {
		input.PlanningConcurrency = 3
	}
	if input.ImplementationConcurrency == 0 {
		input.ImplementationConcurrency = 1
	}
	return storage.WorkStartInput{
		Mode:                      input.Mode,
		ImplementationAdapter:     input.Adapter,
		PlanningConcurrency:       input.PlanningConcurrency,
		ImplementationConcurrency: input.ImplementationConcurrency,
		Until:                     input.Until,
	}, true
}

type artifactApprovalBody struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Notes   string `json:"notes"`
}

func decodeArtifactApprovalBody(w http.ResponseWriter, r *http.Request) (artifactApprovalBody, bool) {
	var input artifactApprovalBody
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return artifactApprovalBody{}, false
	}
	if input.Version <= 0 {
		input.Version = 1
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "approved"
	}
	return input, true
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

func decodeDependencyApprovalBody(w http.ResponseWriter, r *http.Request) (storage.DependencyApprovalRequestInput, bool) {
	var input storage.DependencyApprovalRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return storage.DependencyApprovalRequestInput{}, false
	}
	return input, true
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
