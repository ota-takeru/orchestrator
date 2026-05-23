package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type GitDryRunResult struct {
	MergeQueueEntryID string   `json:"merge_queue_entry_id"`
	TaskID            string   `json:"task_id"`
	RunID             string   `json:"run_id"`
	Status            string   `json:"status"`
	Blockers          []string `json:"blockers,omitempty"`
}

func (db *DB) RunMergeGitDryRun(ctx context.Context, projectID string, entryID string) (GitDryRunResult, error) {
	entry, err := db.openMergeEntryForGitDryRun(ctx, projectID, entryID)
	if err != nil {
		return GitDryRunResult{}, err
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
	if err != nil {
		return GitDryRunResult{}, err
	}
	if strings.TrimSpace(env.ProjectRoot) == "" {
		return GitDryRunResult{}, fmt.Errorf("primary environment project_root is required")
	}
	runID := "RUN-" + stableShortHash(entry.TaskID+"|git-dry-run|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, entry.TaskID, "merge")
	if err != nil {
		return GitDryRunResult{}, err
	}
	commands := []verifier.Command{
		gitDryRunCommand("git-root", env.ID, env.ProjectRoot, "rev-parse", "--show-toplevel"),
		gitDryRunCommand("git-status", env.ID, env.ProjectRoot, "status", "--porcelain=v1"),
		gitDryRunCommand("git-head", env.ID, env.ProjectRoot, "rev-parse", "HEAD"),
		gitDryRunCommand("git-verify-base", env.ID, env.ProjectRoot, "rev-parse", "--verify", entry.BaseCommit+"^{commit}"),
		gitDryRunCommand("git-verify-head", env.ID, env.ProjectRoot, "rev-parse", "--verify", entry.HeadCommit+"^{commit}"),
	}
	localRunner := runners.NewLocalRunner(env)
	results := make([]runners.RunCommandResult, 0, len(commands))
	blockers := []string{}
	for _, command := range commands {
		result, err := localRunner.RunCommand(ctx, runners.RunCommandRequest{
			EnvironmentID:     command.EnvironmentID,
			Runner:            "direct",
			CWD:               command.WorkingDir,
			Argv:              command.Argv,
			Timeout:           15 * time.Second,
			NetworkPolicy:     runners.NetworkOff,
			CaptureStdout:     true,
			CaptureStderr:     true,
			RedactionRequired: true,
			ShellInvocation:   false,
			CommandKind:       "git",
		})
		if err != nil {
			return GitDryRunResult{}, err
		}
		results = append(results, result)
		if result.Status != runners.CommandSucceeded {
			blockers = append(blockers, command.ID+" failed")
		}
		if command.ID == "git-status" && strings.TrimSpace(result.Stdout) != "" {
			blockers = append(blockers, "worktree has uncommitted changes")
		}
	}
	runStatus := "succeeded"
	if len(blockers) > 0 {
		runStatus = "failed"
	}
	if err := db.saveGitDryRun(ctx, projectID, entry, runID, attemptNo, runStatus, commands, results, blockers); err != nil {
		return GitDryRunResult{}, err
	}
	return GitDryRunResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, RunID: runID, Status: runStatus, Blockers: blockers}, nil
}

func gitDryRunCommand(id string, environmentID string, cwd string, args ...string) verifier.Command {
	return verifier.Command{
		ID:            id,
		EnvironmentID: environmentID,
		Runner:        "direct",
		WorkingDir:    cwd,
		Argv:          append([]string{"git"}, args...),
		NetworkPolicy: runners.NetworkOff,
	}
}

func (db *DB) openMergeEntryForGitDryRun(ctx context.Context, projectID string, entryID string) (MergeQueueEntry, error) {
	if strings.TrimSpace(entryID) != "" {
		return db.mergeEntryByID(ctx, projectID, entryID, "queued")
	}
	return db.nextQueuedMergeEntry(ctx, projectID)
}

func (db *DB) nextRunAttempt(ctx context.Context, projectID string, taskID string, runType string) (int, error) {
	var attempt sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, "SELECT MAX(attempt_no) + 1 FROM runs WHERE project_id = ? AND task_id = ? AND run_type = ?", projectID, taskID, runType).Scan(&attempt); err != nil {
		return 0, err
	}
	if !attempt.Valid {
		return 1, nil
	}
	return int(attempt.Int64), nil
}

func (db *DB) saveGitDryRun(ctx context.Context, projectID string, entry MergeQueueEntry, runID string, attemptNo int, runStatus string, commands []verifier.Command, results []runners.RunCommandResult, blockers []string) error {
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
		TaskID:     &entry.TaskID,
		RunID:      runID,
		RunType:    "merge",
		AttemptNo:  attemptNo,
		BaseCommit: entry.BaseCommit,
	}, runStatus, now); err != nil {
		return err
	}
	for i, command := range commands {
		result := results[i]
		commandEventID := commandEventID(runID, command.ID, command.EnvironmentID)
		stdoutArtifactID, stderrArtifactID, err := db.saveCommandOutputArtifacts(ctx, tx, projectID, runID, commandEventID, command.ID, command.EnvironmentID, result, now)
		if err != nil {
			return err
		}
		if err := insertCommandEvent(ctx, tx, projectID, runID, commandEventID, "git", command, result, stdoutArtifactID, stderrArtifactID, now); err != nil {
			return err
		}
	}
	summary, err := json.MarshalIndent(map[string]any{
		"merge_queue_entry_id": entry.ID,
		"task_id":              entry.TaskID,
		"status":               runStatus,
		"blockers":             blockers,
	}, "", "  ")
	if err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        runID,
		ArtifactType: "summary",
		ArtifactKey:  "git-dry-run-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "merge_git_dry_run", map[string]any{
		"task_id":              entry.TaskID,
		"merge_queue_entry_id": entry.ID,
		"run_id":               runID,
		"status":               runStatus,
		"blockers":             blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
