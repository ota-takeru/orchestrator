package storage

import (
	"context"
	"testing"
)

func TestQueueTaskForMergeCreatesEntryAndSyncsTaskStatus(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
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
	if entry.Status != "queued" || entry.BaseCommit != "BASE" || entry.HeadCommit != "HEAD" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
	var status string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued_for_merge" {
		t.Fatalf("task status = %s", status)
	}
}

func TestQueueTaskForMergeRequiresApprovedTask(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	if _, err := db.QueueTaskForMerge(ctx, "PROJECT-001", "TASK-001"); err == nil {
		t.Fatal("expected queueing unapproved task to fail")
	}
}

func TestListMergeQueue(t *testing.T) {
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
	entries, err := db.ListMergeQueue(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TaskID != "TASK-001" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}
