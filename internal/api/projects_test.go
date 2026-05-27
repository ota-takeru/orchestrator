package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/projecthub"
	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/storage"
	_ "modernc.org/sqlite"
)

func TestProjectsAPIListsRegisteredProjects(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	if _, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:      "Windows App",
		AuthorityRuntime: registry.AuthorityWindows,
		ProjectRoot:      t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:        "WSL App",
		AuthorityRuntime:   registry.AuthorityWSL,
		WSLDistro:          "Ubuntu",
		WSLProjectRoot:     "/home/user/app",
		WindowsDisplayRoot: `\\wsl$\Ubuntu\home\user\app`,
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Projects []registry.RegisteredProject `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Projects) != 2 {
		t.Fatalf("projects = %#v", body.Projects)
	}
}

func TestProjectsAPIListsEmptyProjectsAsArray(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Projects []registry.RegisteredProject `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Projects == nil {
		t.Fatalf("projects is nil: %s", rec.Body.String())
	}
	if len(body.Projects) != 0 {
		t.Fatalf("projects = %#v", body.Projects)
	}
}

func TestProjectPathSuggestInfersRootFromName(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/project-paths/suggest?name=My%20Cool%20App&runtime=windows", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var suggestion struct {
		Slug        string `json:"slug"`
		ProjectRoot string `json:"project_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suggestion); err != nil {
		t.Fatal(err)
	}
	if suggestion.Slug != "my-cool-app" || filepath.Base(suggestion.ProjectRoot) != "my-cool-app" {
		t.Fatalf("suggestion = %#v", suggestion)
	}
}

func TestProjectPathSuggestUsesDefaultProjectName(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/project-paths/suggest?runtime=windows", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var suggestion struct {
		DisplayName string `json:"display_name"`
		ProjectRoot string `json:"project_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suggestion); err != nil {
		t.Fatal(err)
	}
	if suggestion.DisplayName != "New Project" || filepath.Base(suggestion.ProjectRoot) != "new-project" {
		t.Fatalf("suggestion = %#v", suggestion)
	}
}

func TestProjectPathBrowseAliasListsDirectories(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/path-browse?path="+url.QueryEscape(root), nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var browse struct {
		Entries []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &browse); err != nil {
		t.Fatal(err)
	}
	if len(browse.Entries) != 1 || browse.Entries[0].Name != "child" {
		t.Fatalf("browse = %#v", browse)
	}
}

func TestExistingDirectoryForExplorerFallsBackToParent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "new-project")
	got, err := existingDirectoryForExplorer(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %s want %s", got, root)
	}
}

func TestProjectPathPickUsesSelectedBaseAndName(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	base := t.TempDir()
	original := projectPathPicker
	projectPathPicker = func(path string) (string, error) {
		if strings.TrimSpace(path) == "" {
			t.Fatal("path was empty")
		}
		return base, nil
	}
	t.Cleanup(func() { projectPathPicker = original })
	body := []byte(`{"path":` + quoteJSON(filepath.Join(base, "old")) + `,"name":"Picked App","runtime":"windows"}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/project-paths/pick", bytes.NewReader(body))
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var suggestion struct {
		BasePath    string `json:"base_path"`
		ProjectRoot string `json:"project_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suggestion); err != nil {
		t.Fatal(err)
	}
	if suggestion.BasePath != base || filepath.Base(suggestion.ProjectRoot) != "picked-app" {
		t.Fatalf("suggestion = %#v", suggestion)
	}
}

func TestProjectsAPICreatesProjectWithSuggestedRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for project creation")
	}
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	currentRoot := filepath.Join(t.TempDir(), "current")
	if err := os.MkdirAll(currentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), "UPDATE projects SET root_path = ? WHERE id = ?", currentRoot, projectID); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{
		"display_name":"Suggested App",
		"project_root":"",
		"concept":"Build a small local project from the UI.",
		"authority_runtime":"windows",
		"generate_initial_artifacts":true
	}`)
	server := NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Project struct {
			ProjectRoot string `json:"project_root"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(created.Project.ProjectRoot) != "suggested-app" {
		t.Fatalf("project root = %s", created.Project.ProjectRoot)
	}
	if _, err := os.Stat(filepath.Join(created.Project.ProjectRoot, ".git")); err != nil {
		t.Fatalf("suggested project root was not initialized: %v", err)
	}
	if status := gitStatusShort(t, created.Project.ProjectRoot); status != "" {
		t.Fatalf("created project should be git clean, status:\n%s", status)
	}
}

func TestProjectsAPICreatesProjectFromUI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for project creation")
	}
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	root := filepath.Join(t.TempDir(), "new-app")
	body := []byte(`{
		"display_name":"New App",
		"project_root":` + quoteJSON(root) + `,
		"concept":"Build a small local project from the UI.",
		"authority_runtime":"wsl",
		"wsl_distro":"Ubuntu",
		"generate_initial_artifacts":true
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Project struct {
			DisplayName      string                    `json:"display_name"`
			AuthorityRuntime registry.AuthorityRuntime `json:"authority_runtime"`
			ProjectRoot      string                    `json:"project_root"`
			WSLDistro        string                    `json:"wsl_distro"`
		} `json:"project"`
		Artifacts []struct {
			ArtifactType string `json:"artifact_type"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Project.DisplayName != "New App" || created.Project.AuthorityRuntime != registry.AuthorityWSL || created.Project.WSLDistro != "Ubuntu" {
		t.Fatalf("project = %#v", created.Project)
	}
	if created.Project.ProjectRoot != root {
		t.Fatalf("project root = %s", created.Project.ProjectRoot)
	}
	if len(created.Artifacts) != 4 {
		t.Fatalf("artifacts = %#v", created.Artifacts)
	}
	for _, rel := range []string{".git", ".gitattributes", ".gitignore", ".devagent/concept.md", ".devagent/prd.md", ".devagent/architecture.md", ".devagent/roadmap.yaml", ".devagent/tasks/TASK-001.yaml", "orchestrator-data/devos.sqlite"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s missing: %v", rel, err)
		}
	}
	if status := gitStatusShort(t, root); status != "" {
		t.Fatalf("created project should be git clean, status:\n%s", status)
	}
	projects, err := regDB.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].DisplayName != "New App" {
		t.Fatalf("projects = %#v", projects)
	}
}

func TestProjectsAPIRoutesToAuthorityRuntime(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	windowsProject, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:      "Windows App",
		AuthorityRuntime: registry.AuthorityWindows,
		ProjectRoot:      t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wslProject, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:      "WSL App",
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   "/home/user/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	windows := apiFakeAuthority{name: "windows"}
	wsl := apiFakeAuthority{name: "wsl"}
	handler := NewServerWithHub(db, projectID, projecthub.NewHub(regDB, windows, wsl)).Handler()

	for _, tc := range []struct {
		id   string
		want string
	}{
		{windowsProject.ID, "windows"},
		{wslProject.ID, "wsl"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/projects/"+tc.id+"/snapshot", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", tc.id, rec.Code, rec.Body.String())
		}
		var snapshot projecthub.ProjectSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.ProjectID != tc.want {
			t.Fatalf("snapshot = %#v want %s", snapshot, tc.want)
		}
	}
}

func TestProjectsAPIRoutesSetupActionToAuthority(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	wslProject, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:      "WSL App",
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   "/home/user/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+wslProject.ID+"/setup/actions/doctor", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{name: "windows"}, apiFakeAuthority{name: "wsl"})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != "wsl" {
		t.Fatalf("body = %#v", body)
	}
}

func TestSetupActionCommitsInitialStateFromUI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for setup action")
	}
	db, projectID := openAPITestDB(t)
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(out))
	}
	if err := os.MkdirAll(filepath.Join(root, ".devagent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devagent", "prd.md"), []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".env.local\n.env.*\norchestrator-data/\n.devagent-worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("* text=auto eol=lf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), "UPDATE projects SET root_path = ? WHERE id = ?", root, projectID); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup/actions/commit_initial_state", nil)
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if status := gitStatusShort(t, root); status != "" {
		t.Fatalf("setup action should make project git clean, status:\n%s", status)
	}
}

func TestProjectsAPIUnknownProjectReturns404(t *testing.T) {
	db, projectID := openAPITestDB(t)
	regDB := openAPIRegistry(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/NOPE/snapshot", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{}, apiFakeAuthority{})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestProjectsAPIInvalidAuthorityReturnsValidationError(t *testing.T) {
	db, projectID := openAPITestDB(t)
	path := filepath.Join(t.TempDir(), "registry.sqlite")
	regDB, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := regDB.Close(); err != nil {
			t.Fatal(err)
		}
	})
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(context.Background(), `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(context.Background(), `
INSERT INTO registered_projects(
  id, display_name, authority_runtime, primary_environment_id, project_root,
  status, created_at, updated_at
) VALUES ('PROJECT-BAD', 'Bad', 'bad', 'bad-main', '/repo', 'active', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/PROJECT-BAD/snapshot", nil)
	NewServerWithHub(db, projectID, projecthub.NewHub(regDB, apiFakeAuthority{}, apiFakeAuthority{})).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func openAPIRegistry(t *testing.T) *registry.DB {
	t.Helper()
	regDB, err := registry.Open(context.Background(), filepath.Join(t.TempDir(), "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := regDB.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return regDB
}

func quoteJSON(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func gitStatusShort(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all").Output()
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

type apiFakeAuthority struct {
	name string
}

func (a apiFakeAuthority) Snapshot(context.Context, registry.RegisteredProject) (projecthub.ProjectSnapshot, error) {
	return projecthub.ProjectSnapshot{ProjectID: a.name}, nil
}
func (a apiFakeAuthority) Dashboard(context.Context, registry.RegisteredProject) (storage.ProjectDashboardData, error) {
	return storage.ProjectDashboardData{Snapshot: projecthub.ProjectSnapshot{ProjectID: a.name}}, nil
}
func (a apiFakeAuthority) Tasks(context.Context, registry.RegisteredProject) (any, error) {
	return map[string]any{"tasks": []any{a.name}}, nil
}
func (a apiFakeAuthority) Inbox(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"items": []any{a.name}}, nil
}
func (a apiFakeAuthority) CreateFeatureRequest(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) CreateChangeRequest(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) AnalyzeChangeRequest(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ApproveChangeRequest(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) StartWork(context.Context, registry.RegisteredProject, storage.WorkStartInput) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) Artifacts(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"artifacts": []any{a.name}}, nil
}
func (a apiFakeAuthority) ApproveArtifact(context.Context, registry.RegisteredProject, string, int, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ReviseArtifact(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ReviseArtifactWithCodex(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) MaterializeTasks(context.Context, registry.RegisteredProject) (any, error) {
	return map[string]any{"tasks": []any{a.name}}, nil
}
func (a apiFakeAuthority) ApproveInboxItem(context.Context, registry.RegisteredProject, string, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) SaveEnvBinding(context.Context, registry.RegisteredProject, storage.EnvBindingInput) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) TaskArtifacts(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"artifacts": []any{a.name}}, nil
}
func (a apiFakeAuthority) SetupStatus(context.Context, registry.RegisteredProject) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) SetupAction(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) VerifyTask(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ApproveTaskReview(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) RejectTaskReview(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ApproveTaskMerge(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) ProcessRealGitMerge(context.Context, registry.RegisteredProject, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
func (a apiFakeAuthority) RequestDependencyApproval(context.Context, registry.RegisteredProject, storage.DependencyApprovalRequestInput) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
