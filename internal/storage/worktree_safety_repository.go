package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type WorktreeSafetyRecord struct {
	RunID        string   `json:"run_id"`
	TaskID       string   `json:"task_id"`
	WorktreePath string   `json:"worktree_path"`
	Status       string   `json:"status"`
	Blockers     []string `json:"blockers,omitempty"`
}

func (db *DB) RunWorktreeSafetyCheck(ctx context.Context, projectID string, taskID string, worktreePath string) (WorktreeSafetyRecord, error) {
	if strings.TrimSpace(taskID) == "" {
		return WorktreeSafetyRecord{}, fmt.Errorf("task id is required")
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
	if err != nil {
		return WorktreeSafetyRecord{}, err
	}
	if strings.TrimSpace(worktreePath) == "" {
		worktreePath = defaultWorktreePath(env, taskID)
	}
	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "worktree_safety")
	if err != nil {
		return WorktreeSafetyRecord{}, err
	}
	blockers := []string{}
	if err := db.ValidateWorktreePath(ctx, projectID, env.ID, worktreePath); err != nil {
		blockers = append(blockers, "invalid worktree path: "+err.Error())
	}
	if ok, err := db.hasSavedDiffArtifact(ctx, projectID, taskID); err != nil {
		return WorktreeSafetyRecord{}, err
	} else if !ok {
		blockers = append(blockers, "diff artifact is not saved")
	}
	if terminal, err := db.taskIsCleanupTerminal(ctx, projectID, taskID); err != nil {
		return WorktreeSafetyRecord{}, err
	} else if !terminal {
		blockers = append(blockers, "task is not in a cleanup-eligible terminal state")
	}

	var command *verifier.Command
	var commandResult *runners.RunCommandResult
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			blockers = append(blockers, "worktree path does not exist")
		} else {
			blockers = append(blockers, "worktree path is not readable: "+err.Error())
		}
	} else {
		cmd := gitWorktreeSafetyCommand(env.ID, worktreePath)
		result, err := runners.NewLocalRunner(env).RunCommand(ctx, runners.RunCommandRequest{
			EnvironmentID:     cmd.EnvironmentID,
			Runner:            "direct",
			CWD:               cmd.WorkingDir,
			Argv:              cmd.Argv,
			Timeout:           15 * time.Second,
			NetworkPolicy:     runners.NetworkOff,
			CaptureStdout:     true,
			CaptureStderr:     true,
			RedactionRequired: true,
			ShellInvocation:   false,
			CommandKind:       "git",
		})
		if err != nil {
			return WorktreeSafetyRecord{}, err
		}
		command = &cmd
		commandResult = &result
		if result.Status != runners.CommandSucceeded {
			blockers = append(blockers, "git status failed")
		}
		if strings.TrimSpace(result.Stdout) != "" {
			blockers = append(blockers, "worktree has uncommitted or untracked changes")
		}
	}

	status := "succeeded"
	if len(blockers) > 0 {
		status = "failed"
	}
	runID := "RUN-" + stableShortHash(taskID+"|worktree-safety|"+time.Now().UTC().Format(time.RFC3339Nano))
	record := WorktreeSafetyRecord{RunID: runID, TaskID: taskID, WorktreePath: worktreePath, Status: status, Blockers: blockers}
	if err := db.saveWorktreeSafetyRecord(ctx, projectID, env.ID, record, attemptNo, command, commandResult); err != nil {
		return WorktreeSafetyRecord{}, err
	}
	return record, nil
}

func defaultWorktreePath(env platform.ExecutionEnvironment, taskID string) string {
	switch env.OSFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		return strings.TrimRight(env.ProjectRoot, `\`) + `\.devagent-worktrees\` + taskID
	default:
		return strings.TrimRight(env.ProjectRoot, "/") + "/.devagent-worktrees/" + taskID
	}
}

func gitWorktreeSafetyCommand(environmentID string, cwd string) verifier.Command {
	return verifier.Command{
		ID:            "git-worktree-status",
		EnvironmentID: environmentID,
		Runner:        "direct",
		WorkingDir:    cwd,
		Argv:          []string{"git", "status", "--porcelain=v1", "--untracked-files=all"},
		NetworkPolicy: runners.NetworkOff,
	}
}

func (db *DB) hasSavedDiffArtifact(ctx context.Context, projectID string, taskID string) (bool, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM run_artifacts ra
JOIN runs r ON r.id = ra.run_id
WHERE ra.project_id = ? AND r.task_id = ? AND ra.artifact_type = 'diff'`, projectID, taskID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) taskIsCleanupTerminal(ctx context.Context, projectID string, taskID string) (bool, error) {
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return false, err
	}
	switch status {
	case "merged", "applied", "cancelled", "failed":
		return true, nil
	default:
		return false, nil
	}
}

func (db *DB) saveWorktreeSafetyRecord(ctx context.Context, projectID string, environmentID string, record WorktreeSafetyRecord, attemptNo int, command *verifier.Command, commandResult *runners.RunCommandResult) error {
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
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &record.TaskID,
		RunID:      record.RunID,
		RunType:    "worktree_safety",
		AttemptNo:  attemptNo,
		BaseCommit: "worktree-safety",
	}, record.Status, now); err != nil {
		return err
	}
	if command != nil && commandResult != nil {
		commandEventID := commandEventID(record.RunID, command.ID, environmentID)
		stdoutArtifactID, stderrArtifactID, err := db.saveCommandOutputArtifacts(ctx, tx, projectID, record.RunID, commandEventID, command.ID, environmentID, *commandResult, now)
		if err != nil {
			return err
		}
		if err := insertCommandEvent(ctx, tx, projectID, record.RunID, commandEventID, "git", *command, *commandResult, stdoutArtifactID, stderrArtifactID, now); err != nil {
			return err
		}
	}
	summary, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        record.RunID,
		ArtifactType: "summary",
		ArtifactKey:  "worktree-safety-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "worktree_safety_checked", map[string]any{
		"task_id":       record.TaskID,
		"run_id":        record.RunID,
		"status":        record.Status,
		"worktree_path": record.WorktreePath,
		"blockers":      record.Blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
