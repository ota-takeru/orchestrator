package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRunArtifactWritesFileAndDBRecord(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertRunForGate(t, db, "PROJECT-001", "RUN-001")

	record, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        "RUN-001",
		ArtifactType: "command_stdout",
		ArtifactKey:  "go-test.stdout.txt",
		Content:      []byte("ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(db.DataRoot(), record.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "ok" {
		t.Fatalf("artifact content = %q", content)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE id = ?", record.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("run artifact count = %d", count)
	}
}

func TestSaveRunArtifactUpdatesExistingKey(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertRunForGate(t, db, "PROJECT-001", "RUN-001")
	input := RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        "RUN-001",
		ArtifactType: "command_stdout",
		ArtifactKey:  "go-test.stdout.txt",
		Content:      []byte("ok"),
	}
	first, err := db.SaveRunArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Content = []byte("updated")
	second, err := db.SaveRunArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ContentHash == second.ContentHash {
		t.Fatalf("unexpected records first=%#v second=%#v", first, second)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE run_id = 'RUN-001'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("run artifact count = %d", count)
	}
}
