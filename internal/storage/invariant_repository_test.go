package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestCheckArtifactInvariantsPassesForApprovedContext(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", t.TempDir(), "approved")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsBrokenApprovedReference(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "UPDATE artifacts SET approved_version_id = ? WHERE id = ?", second.VersionID, first.ArtifactID); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "approved_version_reference_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsReadyTaskWithoutTrustedArtifacts(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "ready_task_missing_trusted_artifacts") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsApprovedWithNotesMissingNotes(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactRoadmap,
		Path:         ".devagent/roadmap.yaml",
		Content:      []byte("roadmap: []"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "UPDATE artifact_versions SET status = 'approved_with_notes', approval_notes = '' WHERE id = ?", record.VersionID); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "approved_with_notes_missing_notes") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidCurrentRun(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "implementing")
	if _, err := db.SQL().ExecContext(ctx, "UPDATE tasks SET current_run_id = 'RUN-MISSING' WHERE id = 'TASK-001'"); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "current_run_reference_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidPrimaryEnvironmentCount(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "primary_environment_count_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidRunProfileEnvironmentJSON(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO project_run_profiles(
  id, project_id, name, mode, status, primary_environment_id,
  implementation_environment_id, git_environment_id, merge_environment_id,
  required_verification_environment_ids_json, optional_verification_environment_ids_json,
  canonical_operations_json, created_at, updated_at
) VALUES (
  'RUNPROFILE-001', 'PROJECT-001', 'default', 'single_environment', 'active', 'linux-main',
  'linux-main', 'linux-main', 'linux-main',
  '{not-json', '[]', '{}', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "run_profile_environment_ids_json_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidTaskVerificationCommands(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE tasks
SET verification_commands_json = ?
WHERE project_id = 'PROJECT-001' AND id = 'TASK-001'`, `[{"id":"sidecar","environment":"missing-env","runner":"auto","required_for_merge":true,"working_dir":"project_root","command":{"argv":[]},"timeout":"not-a-duration","network":false}]`); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "task_verification_command_invalid") || !hasInvariantViolation(violations, "task_verification_environment_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsUnclassifiedRequiredVerificationFailure(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at
) VALUES (
  'RUN-001', 'PROJECT-001', 'TASK-001', 'verification', 'failed', 1, 'BASE',
  ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_id, required_for_merge,
  status, failure_class, evidence_json, created_at
) VALUES (
  'VERIF-001', 'PROJECT-001', 'RUN-001', 'linux-main', 'go-test', 1,
  'failed', NULL, '{}', ?
)`, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "required_verification_failure_unclassified") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidCommandEventJSON(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at
) VALUES (
  'RUN-001', 'PROJECT-001', 'TASK-001', 'verification', 'running', 1, 'BASE',
  ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO command_events(
  id, project_id, run_id, environment_id, command_kind, runner, cwd,
  argv_json, shell_invocation, network_policy, status, detected_risks_json,
  created_at, updated_at, started_at, completed_at
) VALUES (
  'CMD-001', 'PROJECT-001', 'RUN-001', 'linux-main', 'verification', 'direct', '/repo',
  '[]', 0, 'off', 'succeeded', '[]',
  ?, ?, ?, ?
)`, now, now, now, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "command_event_argv_json_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidGateResultSchema(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at
) VALUES (
  'RUN-001', 'PROJECT-001', 'TASK-001', 'verification', 'failed', 1, 'BASE',
  ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO gate_results(
  id, project_id, task_id, run_id, status, severity, detector,
  human_action_type, evidence_json, created_at
) VALUES (
  'GATE-001', 'PROJECT-001', 'TASK-001', 'RUN-001', 'HUMAN_DECISION', 'high', '',
  NULL, '{}', ?
)`, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "gate_result_schema_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsOpenInboxMissingSource(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-MISSING-DECISION', 'PROJECT-001', 'human_decision', 'open', 'decision', 'DEC-MISSING',
  'decision:DEC-MISSING', 80, 'Missing decision', 'Projection without source', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "inbox_source_missing") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsGateInboxProjectionMismatch(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "needs_decision")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at
) VALUES (
  'RUN-001', 'PROJECT-001', 'TASK-001', 'verification', 'failed', 1, 'BASE',
  ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO gate_results(
  id, project_id, task_id, run_id, status, severity, detector,
  human_action_type, evidence_json, created_at
) VALUES (
  'GATE-001', 'PROJECT-001', 'TASK-001', 'RUN-001', 'HARD_BLOCK', 'critical', 'protected_path_write',
  NULL, '{}', ?
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-GATE-001', 'PROJECT-001', 'TASK-001', 'report', 'open', 'gate_result', 'GATE-001',
  'gate:GATE-001', 20, 'Wrong projection', 'Hard block projected as report', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "inbox_gate_projection_mismatch") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsRunArtifactHashMismatch(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectID := "PROJECT-001"
	insertProject(t, db.SQL(), projectID)
	insertTask(t, db, projectID, "TASK-001", "implementing")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at
) VALUES (
  'RUN-001', ?, 'TASK-001', 'implementation', 'succeeded', 1, 'BASE',
  ?, ?
)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}
	artifact, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        "RUN-001",
		ArtifactType: "summary",
		ArtifactKey:  "summary.json",
		Content:      []byte(`{"status":"ok"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(db.dataRoot, artifact.Path), []byte(`{"status":"changed"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckProjectInvariants(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "run_artifact_hash_mismatch") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckProjectInvariantsDetectsInvalidPathMapping(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	insertEnvironmentWithRoot(t, db, "wsl-sidecar", "PROJECT-001", "sidecar", "/sidecar")
	insertPathMapping(t, db, "PROJECT-001", "linux-main", "wsl-sidecar", "/repo", "/mnt/repo", platform.MappingSameFilesystem, "")

	violations, err := db.CheckProjectInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "path_mapping_service_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func hasInvariantViolation(violations []InvariantViolation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}
