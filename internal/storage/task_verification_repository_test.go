package storage

import (
	"context"
	"testing"
)

func TestVerifyTaskFakeAdvancesThroughGate(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")

	result, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "fake"})
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
		t.Fatalf("stored task status = %s", taskStatus)
	}
	var verificationResults, gateResults int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM verification_results WHERE run_id = ?", result.VerificationRun).Scan(&verificationResults); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM gate_results WHERE run_id = ?", result.VerificationRun).Scan(&gateResults); err != nil {
		t.Fatal(err)
	}
	if verificationResults != 1 || gateResults != 1 {
		t.Fatalf("verification_results=%d gate_results=%d", verificationResults, gateResults)
	}
}

func TestVerifyTaskLocalWithoutKnownCommandsNeedsDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")

	result, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" {
		t.Fatalf("task status = %s", result.TaskStatus)
	}
	if len(result.Commands) != 0 {
		t.Fatalf("commands = %#v", result.Commands)
	}
	if len(result.Gates) != 1 || result.Gates[0].Detector != "verification_missing" {
		t.Fatalf("gates = %#v", result.Gates)
	}
}
