package storage

import (
	"context"
	"testing"
	"time"
)

type fakeCodexExecutor struct {
	result CodexExecResult
}

func (f fakeCodexExecutor) ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error) {
	_ = ctx
	if f.result.StartedAt.IsZero() {
		f.result.StartedAt = time.Now().UTC()
	}
	if f.result.CompletedAt.IsZero() {
		f.result.CompletedAt = f.result.StartedAt.Add(time.Second)
	}
	return f.result, nil
}

func TestRunRealCodexTaskRecordsImplementationEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: "done", ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "verifying" || result.Classification != "succeeded" {
		t.Fatalf("result = %#v", result)
	}
	var runStatus, taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = ?", result.ImplementationRun).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "succeeded" || taskStatus != "verifying" {
		t.Fatalf("run=%s task=%s", runStatus, taskStatus)
	}
	var artifactCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE run_id = ?", result.ImplementationRun).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if artifactCount < 4 {
		t.Fatalf("artifact count = %d", artifactCount)
	}
}

func TestRunRealCodexTaskThenVerificationReachesReview(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	runResult, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: "done", ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runResult.TaskStatus != "verifying" {
		t.Fatalf("run result = %#v", runResult)
	}
	verifyResult, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if verifyResult.TaskStatus != "ready_for_human_review" {
		t.Fatalf("verify result = %#v", verifyResult)
	}
}

func TestRunRealCodexTaskFailureOpensDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stderr: "network access is required", ExitCode: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "network_required" {
		t.Fatalf("result = %#v", result)
	}
	var decisionCount, inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM decisions WHERE project_id = 'PROJECT-001' AND task_id = 'TASK-001' AND status = 'open'").Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_type = 'decision' AND status = 'open'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if decisionCount != 1 || inboxCount != 1 {
		t.Fatalf("decision=%d inbox=%d", decisionCount, inboxCount)
	}
}
