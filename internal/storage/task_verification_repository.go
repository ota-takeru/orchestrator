package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
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

var localVerificationRuntimeGOOS = runtime.GOOS

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
	verifiedWorktree := env.ProjectRoot
	verifiedCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	if impl, ok, err := db.latestImplementationVerificationTarget(ctx, projectID, taskID); err != nil {
		return VerifyTaskResult{}, err
	} else if ok {
		env.ProjectRoot = impl.WorktreeRoot
		verifiedWorktree = impl.WorktreeRoot
		verifiedCommit = impl.HeadCommit
	}
	runID := "RUN-" + stableShortHash(taskID+"|verification|"+adapter+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "verification")
	if err != nil {
		return VerifyTaskResult{}, err
	}
	baseCommit := verifiedCommit
	commands, registry, err := db.verificationPlan(ctx, projectID, taskID, adapter, env)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	planHash := verificationPlanHash(commands)
	if err := db.requireVerificationPlan(ctx, projectID, taskID, commands, runID); err != nil {
		return VerifyTaskResult{}, err
	}
	report, err := verifier.Run(ctx, runID, registry, commands)
	if err != nil {
		return VerifyTaskResult{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:            projectID,
		TaskID:               &taskID,
		RunID:                runID,
		RunType:              "verification",
		AttemptNo:            attemptNo,
		BaseCommit:           baseCommit,
		VerifiedWorktree:     verifiedWorktree,
		VerifiedCommit:       verifiedCommit,
		VerificationPlanHash: planHash,
		Commands:             commands,
		Report:               report,
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
	if next == "repairing" {
		if _, err := db.EnqueueTaskRepair(ctx, projectID, taskID, runID); err != nil {
			return VerifyTaskResult{}, err
		}
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

type implementationVerificationTarget struct {
	WorktreeRoot string
	HeadCommit   string
}

func (db *DB) latestImplementationVerificationTarget(ctx context.Context, projectID string, taskID string) (implementationVerificationTarget, bool, error) {
	var target implementationVerificationTarget
	if err := db.sql.QueryRowContext(ctx, `
SELECT ce.cwd, COALESCE(r.head_commit, '')
FROM runs r
JOIN command_events ce ON ce.project_id = r.project_id AND ce.run_id = r.id AND ce.command_kind = 'codex'
WHERE r.project_id = ? AND r.task_id = ? AND r.run_type IN ('implementation', 'repair') AND r.status = 'succeeded'
ORDER BY r.created_at DESC
LIMIT 1`, projectID, taskID).Scan(&target.WorktreeRoot, &target.HeadCommit); err != nil {
		if err == sql.ErrNoRows {
			return implementationVerificationTarget{}, false, nil
		}
		return implementationVerificationTarget{}, false, err
	}
	if strings.TrimSpace(target.WorktreeRoot) == "" {
		return implementationVerificationTarget{}, false, nil
	}
	return target, true, nil
}

func (db *DB) verificationEnvironment(ctx context.Context, projectID string, environmentID string) (platform.ExecutionEnvironment, error) {
	if strings.TrimSpace(environmentID) == "" {
		return db.primaryEnvironment(ctx, projectID)
	}
	var env platform.ExecutionEnvironment
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, os_family, role, shell, project_root, COALESCE(worktree_root, ''), git_provider, codex_adapter, sandbox_profile, status
FROM execution_environments
WHERE project_id = ? AND id = ?
LIMIT 1`, projectID, environmentID).Scan(&env.ID, &env.OSFamily, &env.Role, &env.Shell, &env.ProjectRoot, &env.WorktreeRoot, &env.GitProvider, &env.CodexAdapter, &env.SandboxProfile, &env.Status); err != nil {
		return platform.ExecutionEnvironment{}, err
	}
	return env, nil
}

func (db *DB) verificationPlan(ctx context.Context, projectID string, taskID string, adapter string, env platform.ExecutionEnvironment) ([]verifier.Command, verifier.RunnerRegistry, error) {
	specs, err := db.taskVerificationCommands(ctx, projectID, taskID)
	if err != nil {
		return nil, nil, err
	}
	if len(specs) > 0 {
		return db.verificationPlanFromTaskCommands(ctx, projectID, adapter, env, specs)
	}
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
		if !localVerificationOSSupported(env.OSFamily) {
			return nil, nil, fmt.Errorf("local verification is not supported for %s on %s runtime", env.OSFamily, localVerificationRuntimeGOOS)
		}
		commands := defaultLocalVerificationCommands(ctx, env)
		return commands, verifier.StaticRunnerRegistry{env.ID: runners.NewLocalRunner(env)}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported verification adapter: %s", adapter)
	}
}

func verificationPlanHash(commands []verifier.Command) string {
	raw, err := json.Marshal(commands)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (db *DB) requireVerificationPlan(ctx context.Context, projectID string, taskID string, commands []verifier.Command, runID string) error {
	required := 0
	for _, command := range commands {
		if command.RequiredForMerge {
			required++
		}
	}
	if len(commands) > 0 && required > 0 {
		return nil
	}
	reason := "this project has no required verification command configured"
	if len(commands) > 0 {
		reason = "verification plan has no required-for-merge command"
	}
	if err := db.openVerificationRequiredDecision(ctx, projectID, taskID, runID, reason); err != nil {
		return err
	}
	return fmt.Errorf("verification_required: %s", reason)
}

func (db *DB) openVerificationRequiredDecision(ctx context.Context, projectID string, taskID string, runID string, reason string) error {
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	decisionID := "DEC-" + stableShortHash(projectID+"|"+taskID+"|verification-required")
	options, err := json.Marshal([]map[string]string{
		{"id": "add_required_verification", "label": "Add required verification"},
		{"id": "approve_no_verification_exception", "label": "Approve no-verification exception"},
		{"id": "cancel", "label": "Cancel this task"},
	})
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(map[string]any{"run_id": runID, "classification": "verification_required", "reason": reason})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, task_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES (?, ?, ?, 'open', 'Required verification is missing', ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET status = 'open', evidence_json = excluded.evidence_json, updated_at = excluded.updated_at`,
		decisionID, projectID, taskID, string(options), string(evidence), now, now); err != nil {
		return err
	}
	inboxID := "INBOX-" + stableShortHash(projectID+"|decision|"+decisionID)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, ?, 'human_decision', 'open', 'decision', ?, ?, 85, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at`,
		inboxID, projectID, taskID, decisionID, "decision:"+decisionID,
		"Required verification is missing", reason, now, now); err != nil {
		return err
	}
	if err := statemachine.Task.ValidateTransition("verifying", "needs_decision"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'needs_decision', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'verifying'", now, projectID, taskID); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "verification_required", map[string]any{
		"task_id": taskID,
		"run_id":  runID,
		"reason":  reason,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (db *DB) taskVerificationCommands(ctx context.Context, projectID string, taskID string) ([]TaskVerificationCommand, error) {
	var raw string
	if err := db.sql.QueryRowContext(ctx, "SELECT verification_commands_json FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&raw); err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var commands []TaskVerificationCommand
	if err := json.Unmarshal([]byte(raw), &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

func (db *DB) verificationPlanFromTaskCommands(ctx context.Context, projectID string, adapter string, primaryEnv platform.ExecutionEnvironment, specs []TaskVerificationCommand) ([]verifier.Command, verifier.RunnerRegistry, error) {
	_ = ctx
	commands := make([]verifier.Command, 0, len(specs))
	registry := verifier.StaticRunnerRegistry{}
	for _, spec := range specs {
		env, err := db.environmentForVerificationSpec(ctx, projectID, primaryEnv, spec.Environment)
		if err != nil {
			return nil, nil, err
		}
		runnerName, runner, err := runnerForVerificationAdapter(adapter, env, spec.Runner)
		if err != nil {
			return nil, nil, err
		}
		registry[env.ID] = runner
		command := verifier.Command{
			ID:               spec.ID,
			EnvironmentID:    env.ID,
			Runner:           runnerName,
			WorkingDir:       verificationWorkingDir(env, spec.WorkingDir),
			Argv:             spec.Command.Argv,
			Timeout:          verificationTimeout(spec.Timeout),
			NetworkPolicy:    verificationNetworkPolicy(spec.Network),
			RequiredForMerge: spec.RequiredForMerge,
		}
		if command.ID == "" {
			command.ID = "verification-" + stableShortHash(strings.Join(command.Argv, " "))
		}
		commands = append(commands, command)
	}
	return commands, registry, nil
}

func (db *DB) environmentForVerificationSpec(ctx context.Context, projectID string, primaryEnv platform.ExecutionEnvironment, environment string) (platform.ExecutionEnvironment, error) {
	environment = strings.TrimSpace(environment)
	if environment == "" || environment == "primary" || environment == primaryEnv.ID {
		return primaryEnv, nil
	}
	return db.environmentByID(ctx, projectID, environment)
}

func runnerForVerificationAdapter(adapter string, env platform.ExecutionEnvironment, runner string) (string, runners.Runner, error) {
	runner = strings.TrimSpace(runner)
	switch adapter {
	case "fake":
		if runner == "" || runner == "auto" {
			runner = "fake"
		}
		if runner != "fake" {
			return "", nil, fmt.Errorf("fake verification adapter only supports fake runner")
		}
		return runner, fakeRunnerForEnvironment(env), nil
	case "local":
		if !localVerificationOSSupported(env.OSFamily) {
			return "", nil, fmt.Errorf("local verification is not supported for %s on %s runtime", env.OSFamily, localVerificationRuntimeGOOS)
		}
		if runner == "" || runner == "auto" {
			runner = "direct"
		}
		if runner != "direct" {
			return "", nil, fmt.Errorf("local verification v1 only supports direct runner")
		}
		return runner, runners.NewLocalRunner(env), nil
	default:
		return "", nil, fmt.Errorf("unsupported verification adapter: %s", adapter)
	}
}

func localVerificationOSSupported(osFamily platform.OSFamily) bool {
	switch osFamily {
	case platform.OSFamilyLinux, platform.OSFamilyWSL:
		return true
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		return localVerificationRuntimeGOOS == "windows"
	default:
		return false
	}
}

func verificationWorkingDir(env platform.ExecutionEnvironment, workingDir string) string {
	workingDir = strings.TrimSpace(workingDir)
	switch workingDir {
	case "", "task_worktree", "project_root":
		return env.ProjectRoot
	default:
		if filepath.IsAbs(workingDir) {
			return workingDir
		}
		return filepath.Join(env.ProjectRoot, filepath.FromSlash(workingDir))
	}
}

func verificationTimeout(value string) time.Duration {
	if strings.TrimSpace(value) == "" {
		return 2 * time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 2 * time.Minute
	}
	return duration
}

func verificationNetworkPolicy(network bool) runners.NetworkPolicy {
	if network {
		return runners.NetworkUnrestricted
	}
	return runners.NetworkOff
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
