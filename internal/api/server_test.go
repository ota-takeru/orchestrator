package api

import (
	"bytes"
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

func TestServerApprovesInboxDecision(t *testing.T) {
	db, projectID := openAPITestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO decisions(
  id, project_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES ('DEC-001', ?, 'open', 'Need decision', '[{"id":"A","label":"A"}]', '{}', ?, ?)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-001', ?, 'human_decision', 'open', 'decision', 'DEC-001',
  'decision:DEC-001', 80, 'Need decision', 'Choose option', ?, ?
)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/inbox/INBOX-001/approve", bytes.NewBufferString(`{"option":"A","notes":"ok"}`))
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var result storage.InboxApprovalResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision == nil || result.Decision.Status != "approved" {
		t.Fatalf("approval result = %#v", result)
	}
}

func TestServerListsMemoryByType(t *testing.T) {
	db, projectID := openAPITestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO memories(
  id, project_id, memory_type, key, value, scope, scope_id, source_type, source_id, created_at, updated_at
) VALUES (
  'MEM-BASELINE', ?, 'baseline_issue', 'baseline_issue.GATE-001', '{}',
  'project', '', 'system', 'GATE-001', ?, ?
)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/memory?type=baseline_issue", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Memories []storage.MemoryRecord `json:"memories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Memories) != 1 || body.Memories[0].MemoryType != "baseline_issue" {
		t.Fatalf("memories = %#v", body.Memories)
	}
}

func TestServerExposesTrustedArtifacts(t *testing.T) {
	db, projectID := openAPITestDB(t)
	ctx := context.Background()
	record, err := db.SaveArtifactVersion(ctx, storage.ArtifactVersionInput{
		ProjectID:    projectID,
		ArtifactType: storage.ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, projectID, record.ArtifactID, record.Version, "approved_with_notes", "Keep scope."); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/artifacts/trusted", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Artifacts []storage.TrustedArtifactContentRecord `json:"artifacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Artifacts) != 1 || body.Artifacts[0].ArtifactID != record.ArtifactID || body.Artifacts[0].ApprovalNotes != "Keep scope." || body.Artifacts[0].Content != "# PRD" {
		t.Fatalf("trusted artifacts = %#v", body.Artifacts)
	}
}

func TestServerExposesPathMappings(t *testing.T) {
	db, projectID := openAPITestDB(t)
	ctx := context.Background()
	insertAPIEnvironment(t, db, projectID, "linux-main", "linux", "/repo")
	insertAPIEnvironment(t, db, projectID, "wsl-sidecar", "wsl", "/mnt/repo")
	if _, err := db.SavePathMapping(ctx, storage.PathMappingInput{
		ProjectID:               projectID,
		FromEnvironmentID:       "linux-main",
		ToEnvironmentID:         "wsl-sidecar",
		FromRoot:                "/repo",
		ToRoot:                  "/mnt/repo",
		Mode:                    "same_filesystem",
		WriteOwnerEnvironmentID: "linux-main",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/path-mappings", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Mappings []storage.PathMappingRecord `json:"mappings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Mappings) != 1 || body.Mappings[0].Mode != "same_filesystem" {
		t.Fatalf("path mappings = %#v", body.Mappings)
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

func insertAPIEnvironment(t *testing.T, db *storage.DB, projectID string, envID string, osFamily string, root string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root,
  git_provider, codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (?, ?, ?, 'sidecar', 'bash', ?, 'linux-git', 'codex-linux', 'linux-bubblewrap', 'configured', ?, ?)`,
		envID, projectID, osFamily, root, now, now,
	); err != nil {
		t.Fatal(err)
	}
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
