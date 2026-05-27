package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ota-takeru/orchestrator/internal/storage"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
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

func TestServerApprovesArtifactAndMaterializesTasks(t *testing.T) {
	db, projectID := openAPITestDB(t)
	ctx := context.Background()
	root := t.TempDir()
	if _, err := db.SQL().ExecContext(ctx, "UPDATE projects SET root_path = ? WHERE id = ?", root, projectID); err != nil {
		t.Fatal(err)
	}
	inputs := []storage.ArtifactVersionInput{
		{ProjectID: projectID, ArtifactType: storage.ArtifactPRD, Path: ".devagent/prd.md", Content: []byte("# PRD"), Status: "proposed"},
		{ProjectID: projectID, ArtifactType: storage.ArtifactArchitecture, Path: ".devagent/architecture.md", Content: []byte("# Architecture"), Status: "proposed"},
		{ProjectID: projectID, ArtifactType: storage.ArtifactRoadmap, Path: ".devagent/roadmap.yaml", Content: []byte("roadmap:\n  - TASK-001\n"), Status: "proposed"},
		{ProjectID: projectID, ArtifactType: storage.ArtifactTaskYAML, Path: ".devagent/tasks/TASK-001.yaml", Content: []byte("id: TASK-001\ntitle: Test task\nbase_branch: main\nverification_commands:\n  - id: verify\n    environment: primary\n    runner: auto\n    required_for_merge: true\n    working_dir: project_root\n    command:\n      argv: [\"go\", \"test\", \"./...\"]\n"), Status: "proposed"},
	}
	records := make([]storage.ArtifactVersionRecord, 0, len(inputs))
	for _, input := range inputs {
		if err := writeProjectArtifact(root, input.Path, input.Content); err != nil {
			t.Fatal(err)
		}
		record, err := db.SaveArtifactVersion(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/artifacts", nil)
	listRec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", listRec.Code, listRec.Body.String())
	}

	for _, record := range records {
		req := httptest.NewRequest(http.MethodPost, "/api/artifacts/"+record.ArtifactID+"/approve", bytes.NewBufferString(`{"version":1,"status":"approved"}`))
		rec := httptest.NewRecorder()
		NewServer(db, projectID).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("approve %s status = %d body = %s", record.ArtifactID, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/materialize", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tasks []storage.TaskRecord `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 1 || body.Tasks[0].ID != "TASK-001" || body.Tasks[0].Status != "ready" {
		t.Fatalf("tasks = %#v", body.Tasks)
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

func TestServerExposesToolchainSetupCards(t *testing.T) {
	db, projectID := openAPITestDB(t)
	ctx := context.Background()
	insertAPIEnvironment(t, db, projectID, "linux-main", "linux", "/repo")
	if err := db.SaveToolchainReport(ctx, projectID, toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{{
			ToolchainKey:     "corepack",
			RequiredFor:      toolchains.RequiredForVerification,
			RequiredForMerge: true,
			Status:           toolchains.StatusMissing,
			Message:          "Corepack is missing",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/toolchain-setup", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Cards []storage.ToolchainSetupInstructions `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Cards) != 1 || body.Cards[0].ToolchainKey != "corepack" || len(body.Cards[0].Instructions) == 0 {
		t.Fatalf("toolchain setup cards = %#v", body.Cards)
	}
}

func TestServerExposesMergeStatus(t *testing.T) {
	db, projectID := openAPITestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-MERGE', ?, 'human_decision', 'open', 'decision', 'DEC-MERGE',
  'decision:DEC-MERGE', 80, 'Merge decision', 'Review merge blocker', ?, ?
)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/merge/status", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var status storage.MergeGateStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Ready || len(status.Blockers) == 0 || len(status.BlockingInboxItems) != 1 {
		t.Fatalf("merge status = %#v", status)
	}
}

func TestServerExposesProjectCheck(t *testing.T) {
	db, projectID := openAPITestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Violations []storage.InvariantViolation `json:"violations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !hasAPIViolation(body.Violations, "primary_environment_count_invalid") {
		t.Fatalf("violations = %#v", body.Violations)
	}
}

func TestServerExposesDashboardWorkflowResources(t *testing.T) {
	db, projectID := openAPITestDB(t)
	ctx := context.Background()
	if _, err := db.CreateFeatureRequest(ctx, projectID, "Today Viewを追加して"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateChangeRequest(ctx, projectID, "タスク画面を今日中心に変える"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordDependencyRisk(ctx, storage.DependencyRiskInput{
		ProjectID:        projectID,
		Name:             "zod",
		PackageManager:   "npm",
		DependencyType:   "production",
		Reason:           "UI validation",
		Risk:             "medium",
		LifecycleScripts: "unknown",
		ApprovedScope:    "project",
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path string
		key  string
	}{
		{"/api/requests", "requests"},
		{"/api/queue", "items"},
		{"/api/work/status", "worker_runs"},
		{"/api/planning/status", "runs"},
		{"/api/change-requests", "change_requests"},
		{"/api/dependency-risks", "risks"},
		{"/api/tasks", "tasks"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		NewServer(db, projectID).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", tc.path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body[tc.key]; !ok {
			t.Fatalf("%s missing key %s: %#v", tc.path, tc.key, body)
		}
	}
}

func TestServerRequestsDependencyApproval(t *testing.T) {
	db, projectID := openAPITestDB(t)
	req := httptest.NewRequest(http.MethodPost, "/api/dependency-approvals", bytes.NewBufferString(`{"name":"zod","package_manager":"npm","dependency_type":"production","reason":"schema validation","risk":"medium","alternatives":"manual","files_affected":"package.json"}`))
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body storage.DependencyApprovalRequestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.DecisionID == "" || body.InboxID == "" {
		t.Fatalf("body = %#v", body)
	}
}

func TestServerRunsSetupAction(t *testing.T) {
	db, projectID := openAPITestDB(t)
	req := httptest.NewRequest(http.MethodPost, "/api/setup/actions/fake_workflow", nil)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body storage.SetupActionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ActionID != "fake_workflow" || body.Status != "manual_required" {
		t.Fatalf("body = %#v", body)
	}
}

func TestServerCreatesEnvBindingWithoutReturningSecret(t *testing.T) {
	db, projectID := openAPITestDB(t)
	if _, err := db.SQL().ExecContext(context.Background(), "UPDATE projects SET root_path = ? WHERE id = ?", t.TempDir(), projectID); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"key":"OPENAI_API_KEY","scope":"project","value":"secret-value"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/env/bindings", body)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret-value")) {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
	var record storage.EnvBindingRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Storage != "env_file" || record.Key != "OPENAI_API_KEY" {
		t.Fatalf("binding = %#v", record)
	}
}

func TestServerRequiresLocalTokenForSensitivePostWhenConfigured(t *testing.T) {
	db, projectID := openAPITestDB(t)
	if _, err := db.SQL().ExecContext(context.Background(), "UPDATE projects SET root_path = ? WHERE id = ?", t.TempDir(), projectID); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"key":"OPENAI_API_KEY","scope":"project","value":"secret-value"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/env/bindings", body)
	rec := httptest.NewRecorder()
	NewServer(db, projectID).WithLocalToken("local-test-token").Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	body = bytes.NewBufferString(`{"key":"OPENAI_API_KEY","scope":"project","value":"secret-value"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/env/bindings", body)
	req.Header.Set("X-DevOS-Token", "local-test-token")
	rec = httptest.NewRecorder()
	NewServer(db, projectID).WithLocalToken("local-test-token").Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestServerRestrictsCORSOriginsToLocalhost(t *testing.T) {
	db, projectID := openAPITestDB(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec = httptest.NewRecorder()
	NewServer(db, projectID).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("status = %d allow-origin = %s", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func hasAPIViolation(violations []storage.InvariantViolation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
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

func writeProjectArtifact(root string, relPath string, content []byte) error {
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
