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
