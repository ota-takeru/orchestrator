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

func TestVerifyTaskUsesTaskYAMLVerificationCommands(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "verifying")
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE tasks
SET verification_commands_json = ?
WHERE project_id = 'PROJECT-001' AND id = 'TASK-001'`, `[{"id":"task-smoke","environment":"primary","runner":"auto","required_for_merge":true,"working_dir":"task_worktree","command":{"argv":["verify"]},"network":false}]`); err != nil {
		t.Fatal(err)
	}

	result, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Commands) != 1 || result.Commands[0].ID != "task-smoke" {
		t.Fatalf("commands = %#v", result.Commands)
	}
	if result.TaskStatus != "ready_for_human_review" {
		t.Fatalf("task status = %s", result.TaskStatus)
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
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'human_decision' AND source_type = 'gate_result'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("human decision inbox count = %d", inboxCount)
	}
}
