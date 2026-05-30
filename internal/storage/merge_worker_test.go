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
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE run_type = 'reverify' AND reverify_context_type = 'merge_queue_entry' AND reverify_context_id = ?", result.MergeQueueEntryID).Scan(&reverifyCount); err != nil {
		t.Fatal(err)
	}
	if reverifyCount != 1 {
		t.Fatalf("reverify count = %d", reverifyCount)
	}
}

func TestProcessMergeQueueAutoUsesFakeMergeForSyntheticHead(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	if _, err := db.SQL().ExecContext(ctx, "UPDATE runs SET head_commit = 'HEAD-FAKE' WHERE id = 'RUN-001'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        "RUN-001",
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/fake.txt b/fake.txt\n"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	entry, err := db.QueueTaskForMerge(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.ProcessMergeQueueAuto(ctx, "PROJECT-001", RealGitMergeInput{
		EntryID: entry.ID,
		Target:  "main",
		Execute: true,
		FFOnly:  true,
		NoPush:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.TaskID != "TASK-001" || result.ReverifyRunID == "" {
		t.Fatalf("result = %#v", result)
	}
	var taskStatus, queueStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM merge_queue_entries WHERE id = ?", entry.ID).Scan(&queueStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "merged" || queueStatus != "merged" {
		t.Fatalf("task=%s queue=%s", taskStatus, queueStatus)
	}
}

func TestProcessNextFakeMergeConflictOpensInboxDecision(t *testing.T) {
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

	result, err := db.ProcessNextFakeMergeConflict(ctx, "PROJECT-001", "conflict in fake.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "merge_conflict" {
		t.Fatalf("task status = %s", result.TaskStatus)
	}
	var taskStatus, queueStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM merge_queue_entries WHERE id = ?", result.MergeQueueEntryID).Scan(&queueStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "merge_conflict" || queueStatus != "merge_conflict" {
		t.Fatalf("task=%s queue=%s", taskStatus, queueStatus)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE source_type = 'merge_conflict' AND source_id = ? AND status = 'open'", result.MergeQueueEntryID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count = %d", inboxCount)
	}
}

func TestRetryFakeMergeConflictMovesTaskToMerged(t *testing.T) {
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
	conflict, err := db.ProcessNextFakeMergeConflict(ctx, "PROJECT-001", "conflict in fake.txt")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.RetryFakeMergeConflict(ctx, "PROJECT-001", conflict.MergeQueueEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "merged" {
		t.Fatalf("retry status = %s", result.TaskStatus)
	}
}

func TestCancelMergeConflictCancelsTaskAndResolvesInbox(t *testing.T) {
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
	conflict, err := db.ProcessNextFakeMergeConflict(ctx, "PROJECT-001", "conflict in fake.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry, err := db.CancelMergeConflict(ctx, "PROJECT-001", conflict.MergeQueueEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Status != "cancelled" {
		t.Fatalf("entry status = %s", entry.Status)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("task status = %s", taskStatus)
	}
	var openInboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE source_id = ? AND status = 'open'", conflict.MergeQueueEntryID).Scan(&openInboxCount); err != nil {
		t.Fatal(err)
	}
	if openInboxCount != 0 {
		t.Fatalf("open inbox count = %d", openInboxCount)
	}
}
