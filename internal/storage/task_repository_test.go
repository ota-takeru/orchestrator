package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDraftArtifactsDoNotMakeTaskReady(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	if _, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactTaskYAML,
		Path:         ".devagent/tasks/TASK-001.yaml",
		Content:      []byte("id: TASK-001\ntitle: Task\n"),
		Status:       "proposed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001"); err == nil {
		t.Fatal("expected materialize without approved artifacts to fail")
	}
}

func TestApprovedArtifactsMakeTaskReady(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", root, "approved")
	tasks, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "TASK-001" || tasks[0].Status != "ready" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestRejectedArtifactCannotMaterializeReadyTask(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", root, "approved")
	taskArtifactID := artifactID("PROJECT-001", ArtifactTaskYAML)
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", taskArtifactID, 1, "rejected", "bad task"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001"); err == nil {
		t.Fatal("expected rejected task artifact to fail materialization")
	}
}

func approveRequiredArtifacts(t *testing.T, db *DB, ctx context.Context, projectID string, root string, status string) {
	t.Helper()
	artifacts := []struct {
		typ     ArtifactType
		path    string
		content []byte
	}{
		{ArtifactPRD, ".devagent/prd.md", []byte("# PRD")},
		{ArtifactArchitecture, ".devagent/architecture.md", []byte("# Architecture")},
		{ArtifactRoadmap, ".devagent/roadmap.yaml", []byte("slices: []")},
		{ArtifactTaskYAML, ".devagent/tasks/TASK-001.yaml", []byte("id: TASK-001\ntitle: Bootstrap fake workflow\nbase_branch: main\n")},
	}
	for _, artifact := range artifacts {
		writeTestArtifact(t, root, artifact.path, artifact.content)
		record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
			ProjectID:    projectID,
			ArtifactType: artifact.typ,
			Path:         artifact.path,
			Content:      artifact.content,
			Status:       "proposed",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ApproveArtifactVersion(ctx, projectID, record.ArtifactID, record.Version, status, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func seedProjectRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func insertProjectWithRoot(t *testing.T, db *DB, projectID string, root string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO projects(
  id, name, root_path, lifecycle_status, archive_status, created_at, updated_at
) VALUES (?, 'Project', ?, 'concept', 'active', ?, ?)`, projectID, root, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestArtifact(t *testing.T, root string, relPath string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}
