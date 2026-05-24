package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type FakeRunResult struct {
	TaskID            string `json:"task_id"`
	TaskStatus        string `json:"task_status"`
	ImplementationRun string `json:"implementation_run_id"`
	RepairRun         string `json:"repair_run_id,omitempty"`
	VerificationRun   string `json:"verification_run_id"`
}

func (db *DB) RunFakeTask(ctx context.Context, projectID string, taskID string) (FakeRunResult, error) {
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return FakeRunResult{}, err
	}
	if status != "ready" {
		return FakeRunResult{}, fmt.Errorf("task %s is not ready: %s", taskID, status)
	}
	if err := db.transitionTask(ctx, projectID, taskID, "ready", "implementing", "fake_implementation_started", nil); err != nil {
		return FakeRunResult{}, err
	}

	env, err := db.primaryEnvironment(ctx, projectID)
	if err != nil {
		return FakeRunResult{}, err
	}
	implementationRunID, err := db.createTerminalRun(ctx, projectID, taskID, "implementation", "succeeded", 1, "BASE", "HEAD-"+stableShortHash(taskID), "DIFF-"+stableShortHash(taskID))
	if err != nil {
		return FakeRunResult{}, err
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        implementationRunID,
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/fake.txt b/fake.txt\n"),
	}); err != nil {
		return FakeRunResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "implementing", "verifying", "fake_implementation_completed", map[string]any{"run_id": implementationRunID}); err != nil {
		return FakeRunResult{}, err
	}

	verificationRunID := "RUN-" + stableShortHash(taskID+"|verification|"+time.Now().UTC().Format(time.RFC3339Nano))
	command := verifier.Command{
		ID:               "fake-verification",
		EnvironmentID:    env.ID,
		Runner:           "fake",
		WorkingDir:       env.ProjectRoot,
		Argv:             []string{"verify"},
		NetworkPolicy:    runners.NetworkOff,
		RequiredForMerge: true,
	}
	report, err := verifier.Run(ctx, verificationRunID, verifier.StaticRunnerRegistry{env.ID: fakeRunnerForEnvironment(env)}, []verifier.Command{command})
	if err != nil {
		return FakeRunResult{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &taskID,
		RunID:      verificationRunID,
		AttemptNo:  1,
		BaseCommit: "BASE",
		Commands:   []verifier.Command{command},
		Report:     report,
	}); err != nil {
		return FakeRunResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "verifying", "reviewing", "fake_verification_completed", map[string]any{"run_id": verificationRunID}); err != nil {
		return FakeRunResult{}, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &taskID, verificationRunID, gates); err != nil {
		return FakeRunResult{}, err
	}
	next := taskStatusFromGateResults(gates)
	if err := db.transitionTask(ctx, projectID, taskID, "reviewing", next, "fake_gate_evaluated", map[string]any{"run_id": verificationRunID}); err != nil {
		return FakeRunResult{}, err
	}
	if next == "repairing" {
		if _, err := db.EnqueueTaskRepair(ctx, projectID, taskID, verificationRunID); err != nil {
			return FakeRunResult{}, err
		}
	}
	return FakeRunResult{TaskID: taskID, TaskStatus: next, ImplementationRun: implementationRunID, VerificationRun: verificationRunID}, nil
}

func (db *DB) RunFakeRepairTask(ctx context.Context, projectID string, taskID string) (FakeRunResult, error) {
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return FakeRunResult{}, err
	}
	if status != "repairing" {
		return FakeRunResult{}, fmt.Errorf("task %s is not repairing: %s", taskID, status)
	}
	env, err := db.primaryEnvironment(ctx, projectID)
	if err != nil {
		return FakeRunResult{}, err
	}
	repairAttempt, err := db.nextRunAttempt(ctx, projectID, taskID, "repair")
	if err != nil {
		return FakeRunResult{}, err
	}
	repairRunID, err := db.createTerminalRun(ctx, projectID, taskID, "repair", "succeeded", repairAttempt, "BASE", "HEAD-REPAIR-"+stableShortHash(taskID), "DIFF-REPAIR-"+stableShortHash(taskID))
	if err != nil {
		return FakeRunResult{}, err
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        repairRunID,
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/fake.txt b/fake.txt\n"),
	}); err != nil {
		return FakeRunResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "repairing", "verifying", "fake_repair_completed", map[string]any{"run_id": repairRunID}); err != nil {
		return FakeRunResult{}, err
	}

	verificationRunID := "RUN-" + stableShortHash(taskID+"|repair-verification|"+time.Now().UTC().Format(time.RFC3339Nano))
	verificationAttempt, err := db.nextRunAttempt(ctx, projectID, taskID, "verification")
	if err != nil {
		return FakeRunResult{}, err
	}
	command := verifier.Command{
		ID:               "fake-repair-verification",
		EnvironmentID:    env.ID,
		Runner:           "fake",
		WorkingDir:       env.ProjectRoot,
		Argv:             []string{"verify"},
		NetworkPolicy:    runners.NetworkOff,
		RequiredForMerge: true,
	}
	report, err := verifier.Run(ctx, verificationRunID, verifier.StaticRunnerRegistry{env.ID: fakeRunnerForEnvironment(env)}, []verifier.Command{command})
	if err != nil {
		return FakeRunResult{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &taskID,
		RunID:      verificationRunID,
		RunType:    "verification",
		AttemptNo:  verificationAttempt,
		BaseCommit: "BASE",
		Commands:   []verifier.Command{command},
		Report:     report,
	}); err != nil {
		return FakeRunResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "verifying", "reviewing", "fake_repair_verification_completed", map[string]any{"run_id": verificationRunID}); err != nil {
		return FakeRunResult{}, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &taskID, verificationRunID, gates); err != nil {
		return FakeRunResult{}, err
	}
	next := taskStatusFromGateResults(gates)
	if err := db.transitionTask(ctx, projectID, taskID, "reviewing", next, "fake_repair_gate_evaluated", map[string]any{"run_id": verificationRunID}); err != nil {
		return FakeRunResult{}, err
	}
	return FakeRunResult{TaskID: taskID, TaskStatus: next, RepairRun: repairRunID, VerificationRun: verificationRunID}, nil
}

func (db *DB) taskStatus(ctx context.Context, projectID string, taskID string) (string, error) {
	var status string
	if err := db.sql.QueryRowContext(ctx, "SELECT status FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("task not found: %s", taskID)
		}
		return "", err
	}
	return status, nil
}

func (db *DB) transitionTask(ctx context.Context, projectID string, taskID string, from string, to string, eventType string, evidence any) error {
	if err := statemachine.Task.ValidateTransition(from, to); err != nil {
		return err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&current); err != nil {
		return err
	}
	if current != from {
		return fmt.Errorf("task %s expected status %s, got %s", taskID, from, current)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = ?, updated_at = ? WHERE project_id = ? AND id = ?", to, now, projectID, taskID); err != nil {
		return err
	}
	if evidence == nil {
		evidence = map[string]any{"task_id": taskID}
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, eventType, evidence, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (db *DB) primaryEnvironment(ctx context.Context, projectID string) (platform.ExecutionEnvironment, error) {
	var env platform.ExecutionEnvironment
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, os_family, role, shell, project_root, git_provider, codex_adapter, sandbox_profile, status
FROM execution_environments
WHERE project_id = ? AND role = 'primary'
LIMIT 1`, projectID).Scan(&env.ID, &env.OSFamily, &env.Role, &env.Shell, &env.ProjectRoot, &env.GitProvider, &env.CodexAdapter, &env.SandboxProfile, &env.Status); err != nil {
		if err == sql.ErrNoRows {
			return platform.ExecutionEnvironment{}, fmt.Errorf("primary environment not found for project %s", projectID)
		}
		return platform.ExecutionEnvironment{}, err
	}
	return env, nil
}

func (db *DB) createTerminalRun(ctx context.Context, projectID string, taskID string, runType string, status string, attemptNo int, baseCommit string, headCommit string, diffHash string) (string, error) {
	runID := "RUN-" + stableShortHash(taskID+"|"+runType+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.sql.ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit, head_commit,
  diff_hash, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, projectID, taskID, runType, status, attemptNo, baseCommit, headCommit, diffHash, now, now, now, now,
	)
	return runID, err
}

func fakeRunnerForEnvironment(env platform.ExecutionEnvironment) runners.Runner {
	switch env.OSFamily {
	case platform.OSFamilyWindows:
		return runners.NewFakeWindowsRunner(env.ID)
	case platform.OSFamilyWSL:
		return runners.NewFakeWSLRunner(env.ID)
	default:
		return runners.NewFakeLinuxRunner(env.ID)
	}
}

func taskStatusFromGateResults(results []decisions.GateResult) string {
	next := "ready_for_human_review"
	for _, result := range results {
		switch result.Status {
		case decisions.GateAutoRepair:
			return "repairing"
		case decisions.GateHumanInput:
			return "needs_input"
		case decisions.GateHumanDecision:
			return "needs_decision"
		case decisions.GateHardBlock:
			return "blocked_on_policy"
		case decisions.GateAutoReplan:
			next = "proposed"
		}
	}
	return next
}
