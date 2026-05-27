package registry

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAddListRemoveProjects(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	windowsProject, err := db.AddProject(ctx, AddProjectInput{
		DisplayName:      "Windows App",
		AuthorityRuntime: AuthorityWindows,
		ProjectRoot:      filepath.Join(t.TempDir(), "app"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wslProject, err := db.AddProject(ctx, AddProjectInput{
		DisplayName:        "WSL App",
		AuthorityRuntime:   AuthorityWSL,
		WSLDistro:          "Ubuntu",
		WSLProjectRoot:     "/home/user/app",
		WindowsDisplayRoot: `\\wsl$\Ubuntu\home\user\app`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if windowsProject.AuthorityRuntime != AuthorityWindows || windowsProject.PrimaryEnvironment != "windows-main" {
		t.Fatalf("windows project = %#v", windowsProject)
	}
	if wslProject.AuthorityRuntime != AuthorityWSL || wslProject.PrimaryEnvironment != "wsl-main" {
		t.Fatalf("wsl project = %#v", wslProject)
	}

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("project count = %d", len(projects))
	}

	if _, err := db.UpdateProjectStatus(ctx, windowsProject.ID, StatusActive, true); err != nil {
		t.Fatal(err)
	}
	refreshed, err := db.GetProject(ctx, windowsProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.LastSeenAt == "" {
		t.Fatalf("last_seen_at not updated: %#v", refreshed)
	}

	if err := db.RemoveProject(ctx, wslProject.ID); err != nil {
		t.Fatal(err)
	}
	projects, err = db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("project count after remove = %d", len(projects))
	}
}

func TestListProjectsReturnsEmptySlice(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if projects == nil {
		t.Fatal("projects is nil")
	}
	if len(projects) != 0 {
		t.Fatalf("project count = %d", len(projects))
	}
}

func TestDuplicateAddIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := filepath.Join(t.TempDir(), "app")
	first, err := db.AddProject(ctx, AddProjectInput{DisplayName: "First", AuthorityRuntime: AuthorityWindows, ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.AddProject(ctx, AddProjectInput{DisplayName: "Second", AuthorityRuntime: AuthorityWindows, ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.DisplayName != "Second" {
		t.Fatalf("unexpected idempotent upsert: first=%#v second=%#v", first, second)
	}
}

func TestInvalidAuthorityRuntime(t *testing.T) {
	if _, err := NormalizeProject(AddProjectInput{DisplayName: "Bad", AuthorityRuntime: "linux"}); err == nil {
		t.Fatal("expected invalid authority to fail")
	}
}

func TestWSLProjectIDUsesDistroAndRoot(t *testing.T) {
	first := WSLProjectID("Ubuntu", "/home/user/app")
	second := WSLProjectID("Debian", "/home/user/app")
	if first == second {
		t.Fatal("expected distro to affect WSL project id")
	}
}
