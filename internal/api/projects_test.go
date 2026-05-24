package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/projecthub"
	"github.com/ota-takeru/orchestrator/internal/registry"
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

type apiFakeAuthority struct {
	name string
}

func (a apiFakeAuthority) Snapshot(context.Context, registry.RegisteredProject) (projecthub.ProjectSnapshot, error) {
	return projecthub.ProjectSnapshot{ProjectID: a.name}, nil
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
func (a apiFakeAuthority) ApproveInboxItem(context.Context, registry.RegisteredProject, string, string, string) (any, error) {
	return map[string]any{"ok": a.name}, nil
}
