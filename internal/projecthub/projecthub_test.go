package projecthub

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/storage"
)

func TestHubRoutesByAuthorityRuntime(t *testing.T) {
	windows := fakeAuthority{name: "windows"}
	wsl := fakeAuthority{name: "wsl"}
	hub := NewHub(nil, windows, wsl)

	authority, err := hub.AuthorityFor(registry.RegisteredProject{AuthorityRuntime: registry.AuthorityWindows})
	if err != nil {
		t.Fatal(err)
	}
	if authority.(fakeAuthority).name != "windows" {
		t.Fatalf("authority = %#v", authority)
	}
	authority, err = hub.AuthorityFor(registry.RegisteredProject{AuthorityRuntime: registry.AuthorityWSL})
	if err != nil {
		t.Fatal(err)
	}
	if authority.(fakeAuthority).name != "wsl" {
		t.Fatalf("authority = %#v", authority)
	}
	if _, err := hub.AuthorityFor(registry.RegisteredProject{AuthorityRuntime: "bad"}); err == nil || !strings.Contains(err.Error(), "invalid authority_runtime") {
		t.Fatalf("expected invalid authority error, got %v", err)
	}
}

func TestHubRefreshUpdatesCachedSummary(t *testing.T) {
	regDB, err := registry.Open(context.Background(), filepath.Join(t.TempDir(), "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer regDB.Close()
	project, err := regDB.AddProject(context.Background(), registry.AddProjectInput{
		DisplayName:      "Windows App",
		AuthorityRuntime: registry.AuthorityWindows,
		ProjectRoot:      t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, snapshot, err := NewHub(regDB, fakeAuthority{name: "snapshot"}, fakeAuthority{}).Refresh(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectID != "snapshot" || updated.Status != registry.StatusActive || updated.LastSeenAt == "" {
		t.Fatalf("updated = %#v snapshot = %#v", updated, snapshot)
	}
}

func TestWslAuthorityBuildsSnapshotCommand(t *testing.T) {
	exec := &capturingExecutor{stdout: []byte(`{"project_id":"PROJECT-1","generated_at":"now","counts":{}}`)}
	authority := NewWslAuthority(exec, time.Second)
	project := registry.RegisteredProject{
		AuthorityRuntime:   registry.AuthorityWSL,
		WSLDistro:          "Ubuntu",
		WSLProjectRoot:     "/home/user/app",
		DataRoot:           "/home/user/app/orchestrator-data",
		WindowsDisplayRoot: `\\wsl$\Ubuntu\home\user\app`,
	}
	if _, err := authority.Snapshot(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	want := []string{"-d", "Ubuntu", "--", "devos", "ui", "snapshot", "--project-root", "/home/user/app", "--data-root", "/home/user/app/orchestrator-data", "--json"}
	if strings.Join(exec.args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v want %#v", exec.args, want)
	}
	if strings.Contains(strings.Join(exec.args, " "), `\\wsl$`) {
		t.Fatalf("wsl command used Windows display root: %#v", exec.args)
	}
}

func TestWslAuthorityBuildsDashboardCommand(t *testing.T) {
	exec := &capturingExecutor{stdout: []byte(`{"snapshot":{"project_id":"PROJECT-1","generated_at":"now","counts":{}}}`)}
	authority := NewWslAuthority(exec, time.Second)
	project := registry.RegisteredProject{
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   "/home/user/app",
	}
	if _, err := authority.Dashboard(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	want := []string{"-d", "Ubuntu", "--", "devos", "ui", "dashboard", "--project-root", "/home/user/app", "--json"}
	if strings.Join(exec.args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v want %#v", exec.args, want)
	}
}

func TestWslAuthorityEnvBindingUsesStdin(t *testing.T) {
	exec := &capturingExecutor{stdout: []byte(`{"id":"ENV-BIND-1","key":"OPENAI_API_KEY"}`)}
	authority := NewWslAuthority(exec, time.Second)
	project := registry.RegisteredProject{
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   "/home/user/app",
	}
	if _, err := authority.SaveEnvBinding(context.Background(), project, storage.EnvBindingInput{Key: "OPENAI_API_KEY", Value: "secret-value"}); err != nil {
		t.Fatal(err)
	}
	if exec.stdin != "secret-value" {
		t.Fatalf("stdin = %q", exec.stdin)
	}
	joined := strings.Join(exec.args, " ")
	if strings.Contains(joined, "secret-value") || !strings.Contains(joined, "--value-stdin") {
		t.Fatalf("secret leaked or missing stdin flag: %#v", exec.args)
	}
}

func TestWslAuthorityRejectsUNCProjectRoot(t *testing.T) {
	authority := NewWslAuthority(&capturingExecutor{}, time.Second)
	_, err := authority.Snapshot(context.Background(), registry.RegisteredProject{
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   `\\wsl$\Ubuntu\home\user\app`,
	})
	var authorityErr *AuthorityError
	if !errors.As(err, &authorityErr) || authorityErr.Code != "wsl_project_invalid" {
		t.Fatalf("err = %#v", err)
	}
}

func TestWslAuthorityTimeoutIsStructured(t *testing.T) {
	authority := NewWslAuthority(blockingExecutor{}, time.Millisecond)
	_, err := authority.Snapshot(context.Background(), registry.RegisteredProject{
		AuthorityRuntime: registry.AuthorityWSL,
		WSLDistro:        "Ubuntu",
		WSLProjectRoot:   "/home/user/app",
	})
	var authorityErr *AuthorityError
	if !errors.As(err, &authorityErr) || authorityErr.Code != "wsl_timeout" {
		t.Fatalf("err = %#v", err)
	}
}

type fakeAuthority struct {
	name string
}

func (f fakeAuthority) Snapshot(context.Context, registry.RegisteredProject) (ProjectSnapshot, error) {
	return ProjectSnapshot{ProjectID: f.name}, nil
}
func (f fakeAuthority) Dashboard(context.Context, registry.RegisteredProject) (storage.ProjectDashboardData, error) {
	return storage.ProjectDashboardData{Snapshot: ProjectSnapshot{ProjectID: f.name}}, nil
}
func (f fakeAuthority) Tasks(context.Context, registry.RegisteredProject) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) Inbox(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) CreateFeatureRequest(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) CreateChangeRequest(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) ApproveInboxItem(context.Context, registry.RegisteredProject, string, string, string) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) SaveEnvBinding(context.Context, registry.RegisteredProject, storage.EnvBindingInput) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) TaskArtifacts(context.Context, registry.RegisteredProject, string) (any, error) {
	return map[string]any{"name": f.name}, nil
}
func (f fakeAuthority) SetupStatus(context.Context, registry.RegisteredProject) (any, error) {
	return map[string]any{"name": f.name}, nil
}

type capturingExecutor struct {
	name   string
	args   []string
	stdout []byte
	stderr []byte
	code   int
	err    error
	stdin  string
}

func (e *capturingExecutor) Run(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	e.name = name
	e.args = append([]string(nil), args...)
	return e.stdout, e.stderr, e.code, e.err
}

func (e *capturingExecutor) RunWithInput(_ context.Context, stdin string, name string, args ...string) ([]byte, []byte, int, error) {
	e.stdin = stdin
	return e.Run(context.Background(), name, args...)
}

type blockingExecutor struct{}

func (blockingExecutor) Run(ctx context.Context, _ string, _ ...string) ([]byte, []byte, int, error) {
	<-ctx.Done()
	return nil, nil, -1, ctx.Err()
}
