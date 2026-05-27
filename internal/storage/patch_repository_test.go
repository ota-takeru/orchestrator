package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func TestManualPatchExportMarkAndVerifyApplied(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	exported, err := db.ExportPatch(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if exported.Status != "exported" {
		t.Fatalf("export status = %s", exported.Status)
	}
	applied, err := db.MarkPatchApplied(ctx, "PROJECT-001", "TASK-001", "COMMIT1")
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != "manually_applied" || applied.AppliedCommit != "COMMIT1" {
		t.Fatalf("applied patch = %#v", applied)
	}
	verified, err := db.VerifyAppliedPatchFake(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != "verified" {
		t.Fatalf("verified patch = %#v", verified)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "applied" {
		t.Fatalf("task status = %s", taskStatus)
	}
	var reverifyCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE run_type = 'reverify' AND reverify_context_type = 'patch_application' AND reverify_context_id = ?", exported.ID).Scan(&reverifyCount); err != nil {
		t.Fatal(err)
	}
	if reverifyCount != 1 {
		t.Fatalf("reverify context count = %d", reverifyCount)
	}
}

func TestManualPatchVerifyAppliedLocalUsesTaskVerificationPlan(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	head := gitOutput(t, repo, "rev-parse", "HEAD")
	insertProjectWithRoot(t, db, "PROJECT-001", repo)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", repo)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	commands := []TaskVerificationCommand{{
		ID:               "go-test",
		Environment:      "primary",
		Runner:           "auto",
		RequiredForMerge: true,
		WorkingDir:       "project_root",
		Command:          TaskVerificationCommandCommand{Argv: []string{"go", "test", "./..."}},
	}}
	rawCommands, err := json.Marshal(commands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "UPDATE tasks SET verification_commands_json = ? WHERE project_id = 'PROJECT-001' AND id = 'TASK-001'", string(rawCommands)); err != nil {
		t.Fatal(err)
	}
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-001", head, "DIFF")
	insertVerificationEvidence(t, db, "PROJECT-001", "RUN-001", "linux-main", "VERIF-001")
	gates := []decisions.GateResult{{Status: decisions.GatePass, Severity: decisions.SeverityLow, Detector: "verification_passed", Evidence: map[string]any{"run_id": "RUN-001"}}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-001", gates); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	exported, err := db.ExportPatch(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkPatchApplied(ctx, "PROJECT-001", "TASK-001", head); err != nil {
		t.Fatal(err)
	}

	verified, err := db.VerifyAppliedPatch(ctx, "PROJECT-001", "TASK-001", "local")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != "verified" {
		t.Fatalf("verified patch = %#v", verified)
	}
	var commandRunner, evidenceJSON string
	if err := db.SQL().QueryRowContext(ctx, `
SELECT ce.runner, vr.evidence_json
FROM command_events ce
JOIN verification_results vr ON vr.command_event_id = ce.id AND vr.project_id = ce.project_id
WHERE ce.project_id = 'PROJECT-001' AND ce.command_kind = 'verification'
  AND ce.run_id IN (
    SELECT id FROM runs WHERE run_type = 'reverify' AND reverify_context_type = 'patch_application' AND reverify_context_id = ?
  )
LIMIT 1`, exported.ID).Scan(&commandRunner, &evidenceJSON); err != nil {
		t.Fatal(err)
	}
	if commandRunner != "direct" {
		t.Fatalf("command runner = %s", commandRunner)
	}
	var evidence struct {
		VerifiedCommit string `json:"verified_commit"`
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.VerifiedCommit != head {
		t.Fatalf("verified commit = %s want %s", evidence.VerifiedCommit, head)
	}
}

func TestPatchExportRequiresApprovedForMerge(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	if _, err := db.ExportPatch(ctx, "PROJECT-001", "TASK-001"); err == nil {
		t.Fatal("expected patch export to require approved_for_merge")
	}
}

func TestPatchNeedsDecisionCanRegisterNewAppliedCommit(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	exported, err := db.ExportPatch(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkPatchApplied(ctx, "PROJECT-001", "TASK-001", "COMMIT1"); err != nil {
		t.Fatal(err)
	}
	if err := db.transitionTask(ctx, "PROJECT-001", "TASK-001", "manually_applied", "reverifying", "test_patch_reverify_started", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.updatePatchStatus(ctx, "PROJECT-001", exported.ID, "manually_applied", "verifying"); err != nil {
		t.Fatal(err)
	}
	if err := db.markPatchNeedsDecision(ctx, "PROJECT-001", "TASK-001", exported.ID, "patch mismatch"); err != nil {
		t.Fatal(err)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE source_type = 'patch_application' AND source_id = ? AND status = 'open'", exported.ID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count = %d", inboxCount)
	}

	applied, err := db.MarkPatchApplied(ctx, "PROJECT-001", "TASK-001", "COMMIT2")
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != "manually_applied" || applied.AppliedCommit != "COMMIT2" {
		t.Fatalf("applied patch = %#v", applied)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "manually_applied" {
		t.Fatalf("task status = %s", taskStatus)
	}
	var openInboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE source_type = 'patch_application' AND source_id = ? AND status = 'open'", exported.ID).Scan(&openInboxCount); err != nil {
		t.Fatal(err)
	}
	if openInboxCount != 0 {
		t.Fatalf("open inbox count = %d", openInboxCount)
	}
}
