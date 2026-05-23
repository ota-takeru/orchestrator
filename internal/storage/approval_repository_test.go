package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func TestFinalAndMergeApprovalMovesTaskToApprovedForMerge(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)

	finalReview, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalReview.TaskStatus != "ready_for_human_review" || finalReview.ApprovedForMerge {
		t.Fatalf("unexpected final review result: %#v", finalReview)
	}

	merge, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalMerge,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merge.TaskStatus != "approved_for_merge" || !merge.ApprovedForMerge {
		t.Fatalf("unexpected merge result: %#v", merge)
	}

	var status string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved_for_merge" {
		t.Fatalf("task status = %s", status)
	}
}

func TestMergeApprovalRequiresMatchingFinalReview(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)

	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalMerge,
	}); err == nil {
		t.Fatal("expected merge approval without final review to fail")
	}
}

func TestRejectFinalReviewMovesTaskToNeedsDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	rejection, err := db.RejectTaskFinalReview(ctx, ApprovalInput{
		ProjectID: "PROJECT-001",
		TaskID:    "TASK-001",
		Notes:     "needs changes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejection.TaskStatus != "needs_decision" {
		t.Fatalf("rejection = %#v", rejection)
	}
	var approvalStatus, taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM human_approvals WHERE id = ?", rejection.ID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "rejected" || taskStatus != "needs_decision" {
		t.Fatalf("approval=%s task=%s", approvalStatus, taskStatus)
	}
}

func TestApprovalRequiresGateEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-001", "HEAD", "DIFF")
	insertVerificationEvidence(t, db, "PROJECT-001", "RUN-001", "linux-main", "VERIF-001")

	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	}); err == nil {
		t.Fatal("expected approval without gate evidence to fail")
	}
}

func seedApprovalTaskEvidence(t *testing.T, db *DB, ctx context.Context) {
	t.Helper()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-001", "HEAD", "DIFF")
	insertVerificationEvidence(t, db, "PROJECT-001", "RUN-001", "linux-main", "VERIF-001")
	gates := []decisions.GateResult{{
		Status:   decisions.GatePass,
		Severity: decisions.SeverityLow,
		Detector: "verification_passed",
		Evidence: map[string]any{"run_id": "RUN-001"},
	}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-001", gates); err != nil {
		t.Fatal(err)
	}
}

func insertTask(t *testing.T, db *DB, projectID string, taskID string, status string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO tasks(
  id, project_id, status, title, base_branch, created_at, updated_at
) VALUES (?, ?, ?, 'Task', 'main', ?, ?)`,
		taskID, projectID, status, now(), now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func insertSucceededRun(t *testing.T, db *DB, projectID string, taskID string, runID string, headCommit string, diffHash string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit, head_commit,
  diff_hash, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, 'verification', 'succeeded', 1, 'BASE', ?, ?, ?, ?, ?, ?)`,
		runID, projectID, taskID, headCommit, diffHash, now(), now(), now(), now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func insertVerificationEvidence(t *testing.T, db *DB, projectID string, runID string, envID string, resultID string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_id, required_for_merge,
  status, evidence_json, created_at
) VALUES (?, ?, ?, ?, 'go-test', 1, 'passed', '{}', ?)`,
		resultID, projectID, runID, envID, now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T {
	return &v
}
