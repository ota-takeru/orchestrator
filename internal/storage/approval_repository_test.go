package storage

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestApprovalRequiresBaselineIssueReport(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-BASELINE", "HEAD", "DIFF")
	insertBaselineVerificationEvidence(t, db, "PROJECT-001", "RUN-BASELINE", "linux-main", "VERIF-BASELINE")
	gates := []decisions.GateResult{{
		Status:   decisions.GatePass,
		Severity: decisions.SeverityLow,
		Detector: "verification_passed",
		Evidence: map[string]any{"run_id": "RUN-BASELINE"},
	}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-BASELINE", gates); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	}); err == nil {
		t.Fatal("expected approval without baseline issue report to fail")
	}

	baselineGates := []decisions.GateResult{{
		Status:   decisions.GateReportOnly,
		Severity: decisions.SeverityMedium,
		Detector: "verification_failed_existing_baseline",
		Evidence: map[string]any{"run_id": "RUN-BASELINE"},
	}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-BASELINE", baselineGates); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnclassifiedVerificationFailureBlocksMerge(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-UNKNOWN", "HEAD", "DIFF")
	insertUnknownVerificationEvidence(t, db, "PROJECT-001", "RUN-UNKNOWN", "linux-main", "VERIF-UNKNOWN")
	action := "decision"
	gates := []decisions.GateResult{{
		Status:          decisions.GateHumanDecision,
		Severity:        decisions.SeverityHigh,
		Detector:        "required_verification_unclassified",
		HumanActionType: &action,
		Evidence:        map[string]any{"run_id": "RUN-UNKNOWN"},
	}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-UNKNOWN", gates); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	}); err == nil {
		t.Fatal("expected unclassified verification gate to block approval")
	}
}

func TestHumanDecisionBlocksMerge(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, "PROJECT-001", "TASK-001", "RUN-DECISION", "HEAD", "DIFF")
	insertVerificationEvidence(t, db, "PROJECT-001", "RUN-DECISION", "linux-main", "VERIF-DECISION")
	action := "decision"
	gates := []decisions.GateResult{{
		Status:          decisions.GateHumanDecision,
		Severity:        decisions.SeverityHigh,
		Detector:        "policy_requires_human_decision",
		HumanActionType: &action,
		Evidence:        map[string]any{"run_id": "RUN-DECISION"},
	}}
	if err := db.SaveGateResults(ctx, "PROJECT-001", ptr("TASK-001"), "RUN-DECISION", gates); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{
		ProjectID:    "PROJECT-001",
		TaskID:       "TASK-001",
		ApprovalType: ApprovalFinalReview,
	}); err == nil {
		t.Fatal("expected human decision gate to block approval")
	}
}

func TestApproveHumanApprovalApprovesOpenSourceAndResolvesInbox(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	evidenceJSON := approvalEvidenceJSON(t, db, ctx)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-FINAL", "TASK-001", ApprovalFinalReview, evidenceJSON)

	result, err := db.ApproveHumanApproval(ctx, "PROJECT-001", "APPROVAL-FINAL", "approved from inbox")
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "APPROVAL-FINAL" || result.TaskStatus != "ready_for_human_review" || result.ApprovedForMerge {
		t.Fatalf("approval result = %#v", result)
	}
	var approvalStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM human_approvals WHERE id = 'APPROVAL-FINAL'").Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	var openInbox int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE source_type = 'human_approval' AND source_id = 'APPROVAL-FINAL' AND status = 'open'").Scan(&openInbox); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "approved" || openInbox != 0 {
		t.Fatalf("approval=%s open_inbox=%d", approvalStatus, openInbox)
	}
}

func TestHumanApprovalEvidenceAllowsDifferentJSONFieldOrder(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	var gateID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM gate_results WHERE project_id = 'PROJECT-001' AND run_id = 'RUN-001'").Scan(&gateID); err != nil {
		t.Fatal(err)
	}
	evidenceJSON := fmt.Sprintf(`{"gate_result_ids":["%s"],"verification_result_ids":["VERIF-001"],"diff_hash":"DIFF","head_commit":"HEAD","run_id":"RUN-001","base_commit":"BASE"}`, gateID)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-FINAL", "TASK-001", ApprovalFinalReview, evidenceJSON)

	if _, err := db.ApproveHumanApproval(ctx, "PROJECT-001", "APPROVAL-FINAL", "same evidence"); err != nil {
		t.Fatal(err)
	}
}

func TestRunArtifactHashChangesInvalidateHumanApproval(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	evidenceJSON := approvalEvidenceJSON(t, db, ctx)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-FINAL", "TASK-001", ApprovalFinalReview, evidenceJSON)
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE runs
SET diff_hash = 'DIFF-CHANGED'
WHERE project_id = 'PROJECT-001' AND task_id = 'TASK-001' AND status = 'succeeded'`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveHumanApproval(ctx, "PROJECT-001", "APPROVAL-FINAL", "stale approval"); err == nil {
		t.Fatal("expected stale human approval evidence to fail")
	}
	var approvalStatus, inboxStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM human_approvals WHERE id = 'APPROVAL-FINAL'").Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM inbox_items WHERE id = 'INBOX-APPROVAL-FINAL'").Scan(&inboxStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "open" || inboxStatus != "open" {
		t.Fatalf("approval=%s inbox=%s", approvalStatus, inboxStatus)
	}
}

func TestApproveHumanMergeApprovalMovesTaskWhenFinalReviewApproved(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	evidenceJSON := approvalEvidenceJSON(t, db, ctx)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-FINAL", "TASK-001", ApprovalFinalReview, evidenceJSON)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-MERGE", "TASK-001", ApprovalMerge, evidenceJSON)

	if _, err := db.ApproveHumanApproval(ctx, "PROJECT-001", "APPROVAL-FINAL", ""); err != nil {
		t.Fatal(err)
	}
	result, err := db.ApproveHumanApproval(ctx, "PROJECT-001", "APPROVAL-MERGE", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "approved_for_merge" || !result.ApprovedForMerge {
		t.Fatalf("merge approval result = %#v", result)
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

func approvalEvidenceJSON(t *testing.T, db *DB, ctx context.Context) string {
	t.Helper()
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	evidence, err := collectApprovalEvidence(ctx, tx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func insertOpenHumanApprovalWithInbox(t *testing.T, db *DB, projectID string, approvalID string, taskID string, approvalType ApprovalType, evidenceJSON string) {
	t.Helper()
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO human_approvals(
  id, project_id, task_id, approval_type, status, evidence_json, created_at, updated_at
) VALUES (?, ?, ?, ?, 'open', ?, ?, ?)`,
		approvalID, projectID, taskID, approvalType, evidenceJSON, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id, dedupe_key,
  priority, title, body, created_at, updated_at
) VALUES (?, ?, ?, 'approval', 'open', 'human_approval', ?, ?, 70, 'Approval required', 'Approve source evidence', ?, ?)`,
		"INBOX-"+approvalID, projectID, taskID, approvalID, "human-approval-"+approvalID, now(), now()); err != nil {
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

func insertBaselineVerificationEvidence(t *testing.T, db *DB, projectID string, runID string, envID string, resultID string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_id, required_for_merge,
  status, failure_class, evidence_json, created_at
) VALUES (?, ?, ?, ?, 'go-test', 1, 'failed', 'baseline', '{}', ?)`,
		resultID, projectID, runID, envID, now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func insertUnknownVerificationEvidence(t *testing.T, db *DB, projectID string, runID string, envID string, resultID string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_id, required_for_merge,
  status, failure_class, evidence_json, created_at
) VALUES (?, ?, ?, ?, 'go-test', 1, 'failed', 'unknown', '{}', ?)`,
		resultID, projectID, runID, envID, now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T {
	return &v
}
