package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ota-takeru/orchestrator/internal/storage"
)

func TestServerExposesHumanInboxSnapshot(t *testing.T) {
	db, projectID := openAPITestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-001', ?, 'report', 'open', 'execution_environment', 'linux-main',
  'report:linux-main', 20, 'Report', 'Ready', ?, ?
)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ui/snapshot?limit=5", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var snapshot storage.HumanInboxSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.OpenInboxItems != 1 || len(snapshot.OpenInboxItems) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestServerRejectsInvalidSnapshotLimit(t *testing.T) {
	db, projectID := openAPITestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ui/snapshot?limit=bad", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func openAPITestDB(t *testing.T) (*storage.DB, string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	projectID := "PROJECT-001"
	insertAPIProject(t, db.SQL(), projectID)
	return db, projectID
}

func insertAPIProject(t *testing.T, db *sql.DB, projectID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO projects(
  id, name, root_path, lifecycle_status, archive_status, created_at, updated_at
) VALUES (?, 'Project', '/repo', 'concept', 'active', ?, ?)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}
}
