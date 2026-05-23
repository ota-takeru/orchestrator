package storage

import (
	"context"
	"testing"
)

func TestProcessNextFakeMergeMovesQueuedTaskToMerged(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueueTaskForMerge(ctx, "PROJECT-001", "TASK-001"); err != nil {
		t.Fatal(err)
	}

	result, err := db.ProcessNextFakeMerge(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "merged" {
		t.Fatalf("task status = %s", result.TaskStatus)
	}
	var taskStatus, queueStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM merge_queue_entries WHERE id = ?", result.MergeQueueEntryID).Scan(&queueStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "merged" || queueStatus != "merged" {
		t.Fatalf("task=%s queue=%s", taskStatus, queueStatus)
	}
	var reverifyCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE run_type = 'reverify'").Scan(&reverifyCount); err != nil {
		t.Fatal(err)
	}
	if reverifyCount != 1 {
		t.Fatalf("reverify count = %d", reverifyCount)
	}
}
