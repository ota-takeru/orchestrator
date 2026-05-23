package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type VerifyTaskInput struct {
	Adapter       string
	EnvironmentID string
}

type VerifyTaskResult struct {
	TaskID          string                 `json:"task_id"`
	TaskStatus      string                 `json:"task_status"`
	VerificationRun string                 `json:"verification_run_id"`
	Adapter         string                 `json:"adapter"`
	EnvironmentID   string                 `json:"environment_id"`
	Commands        []verifier.Command     `json:"commands"`
	Gates           []decisions.GateResult `json:"gates"`
	Report          verifier.Report        `json:"report"`
}

func (db *DB) VerifyTask(ctx context.Context, projectID string, taskID string, input VerifyTaskInput) (VerifyTaskResult, error) {
	adapter := strings.TrimSpace(input.Adapter)
	if adapter == "" {
		adapter = "local"
	}
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	if status != "verifying" {
		return VerifyTaskResult{}, fmt.Errorf("task %s is not verifying: %s", taskID, status)
	}
	env, err := db.verificationEnvironment(ctx, projectID, input.EnvironmentID)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	runID := "RUN-" + stableShortHash(taskID+"|verification|"+adapter+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "verification")
	if err != nil {
		return VerifyTaskResult{}, err
	}
	baseCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	commands, registry, err := verificationPlan(ctx, adapter, env)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	report, err := verifier.Run(ctx, runID, registry, commands)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &taskID,
		RunID:      runID,
		RunType:    "verification",
		AttemptNo:  attemptNo,
		BaseCommit: baseCommit,
		Commands:   commands,
		Report:     report,
	}); err != nil {
		return VerifyTaskResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "verifying", "reviewing", "verification_completed", map[string]any{
		"task_id":        taskID,
		"run_id":         runID,
		"adapter":        adapter,
		"environment_id": env.ID,
	}); err != nil {
		return VerifyTaskResult{}, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &taskID, runID, gates); err != nil {
		return VerifyTaskResult{}, err
	}
	next := taskStatusFromGateResults(gates)
	if err := db.transitionTask(ctx, projectID, taskID, "reviewing", next, "verification_gate_evaluated", map[string]any{
		"task_id": taskID,
		"run_id":  runID,
		"gates":   gates,
	}); err != nil {
		return VerifyTaskResult{}, err
	}
	return VerifyTaskResult{
		TaskID:          taskID,
		TaskStatus:      next,
		VerificationRun: runID,
		Adapter:         adapter,
		EnvironmentID:   env.ID,
		Commands:        commands,
		Gates:           gates,
		Report:          report,
	}, nil
}

func (db *DB) verificationEnvironment(ctx context.Context, projectID string, environmentID string) (platform.ExecutionEnvironment, error) {
	if strings.TrimSpace(environmentID) == "" {
		return db.primaryEnvironment(ctx, projectID)
	}
	var env platform.ExecutionEnvironment
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, os_family, role, shell, project_root, git_provider, codex_adapter, sandbox_profile, status
FROM execution_environments
WHERE project_id = ? AND id = ?
LIMIT 1`, projectID, environmentID).Scan(&env.ID, &env.OSFamily, &env.Role, &env.Shell, &env.ProjectRoot, &env.GitProvider, &env.CodexAdapter, &env.SandboxProfile, &env.Status); err != nil {
		return platform.ExecutionEnvironment{}, err
	}
	return env, nil
}

func verificationPlan(ctx context.Context, adapter string, env platform.ExecutionEnvironment) ([]verifier.Command, verifier.RunnerRegistry, error) {
	switch adapter {
	case "fake":
		command := verifier.Command{
			ID:               "fake-verification",
			EnvironmentID:    env.ID,
			Runner:           "fake",
			WorkingDir:       env.ProjectRoot,
			Argv:             []string{"verify"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		}
		return []verifier.Command{command}, verifier.StaticRunnerRegistry{env.ID: fakeRunnerForEnvironment(env)}, nil
	case "local":
		if env.OSFamily != platform.OSFamilyLinux {
			return nil, nil, fmt.Errorf("local verification v1 only supports linux current environment")
		}
		commands := defaultLocalVerificationCommands(ctx, env)
		return commands, verifier.StaticRunnerRegistry{env.ID: runners.NewLocalRunner(env)}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported verification adapter: %s", adapter)
	}
}

func defaultLocalVerificationCommands(ctx context.Context, env platform.ExecutionEnvironment) []verifier.Command {
	_ = ctx
	if _, err := os.Stat(filepath.Join(env.ProjectRoot, "go.mod")); err == nil {
		return []verifier.Command{{
			ID:               "go-test",
			EnvironmentID:    env.ID,
			Runner:           "direct",
			WorkingDir:       env.ProjectRoot,
			Argv:             []string{"go", "test", "./..."},
			Timeout:          2 * time.Minute,
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		}}
	}
	return nil
}
