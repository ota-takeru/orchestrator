package storage

import (
	"context"
	"testing"
)

func TestFakeRunWorkflowMovesReadyTaskToHumanReview(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	result, err := db.RunFakeTask(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "ready_for_human_review" {
		t.Fatalf("task status = %s", result.TaskStatus)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "ready_for_human_review" {
		t.Fatalf("db task status = %s", taskStatus)
	}
	var runCount, gateCount, artifactCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE task_id = 'TASK-001'").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM gate_results WHERE task_id = 'TASK-001'").Scan(&gateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts").Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 2 || gateCount == 0 || artifactCount == 0 {
		t.Fatalf("runs=%d gates=%d artifacts=%d", runCount, gateCount, artifactCount)
	}
}

func TestFakeRunWorkflowRejectsNonReadyTask(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "proposed")
	if _, err := db.RunFakeTask(ctx, "PROJECT-001", "TASK-001"); err == nil {
		t.Fatal("expected non-ready task to fail")
	}
}
