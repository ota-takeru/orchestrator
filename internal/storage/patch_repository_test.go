package storage

import (
	"context"
	"testing"
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
