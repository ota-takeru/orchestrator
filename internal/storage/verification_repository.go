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

type SaveVerificationInput struct {
	ProjectID           string
	TaskID              *string
	RunID               string
	RunType             string
	AttemptNo           int
	BaseCommit          string
	ReverifyContextType string
	ReverifyContextID   string
	Commands            []verifier.Command
	Report              verifier.Report
}

func (db *DB) SaveVerificationReport(ctx context.Context, input SaveVerificationInput) error {
	if strings.TrimSpace(input.ProjectID) == "" {
		return fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.RunID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(input.BaseCommit) == "" {
		return fmt.Errorf("base commit is required")
	}
	if input.AttemptNo <= 0 {
		return fmt.Errorf("attempt_no must be positive")
	}
	commandByID := map[string]verifier.Command{}
	for _, command := range input.Commands {
		commandByID[command.ID] = command
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

	runStatus := "succeeded"
	if input.Report.RequiredFailureCount() > 0 {
		runStatus = "failed"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if input.RunType == "" {
		input.RunType = "verification"
	}
	if input.RunType == "reverify" && (strings.TrimSpace(input.ReverifyContextType) == "" || strings.TrimSpace(input.ReverifyContextID) == "") {
		return fmt.Errorf("reverify context type and id are required")
	}
	if err := insertRun(ctx, tx, input, runStatus, now); err != nil {
		return err
	}
	for _, result := range input.Report.Results {
		command, ok := commandByID[result.CommandID]
		if !ok {
			return fmt.Errorf("verification command not found for result: %s", result.CommandID)
		}
		commandEventID := commandEventID(input.RunID, result.CommandID, result.EnvironmentID)
		stdoutArtifactID, stderrArtifactID, err := db.saveCommandOutputArtifacts(ctx, tx, input.ProjectID, input.RunID, commandEventID, result.CommandID, result.EnvironmentID, result.CommandResult, now)
		if err != nil {
			return err
		}
		if err := insertCommandEvent(ctx, tx, input.ProjectID, input.RunID, commandEventID, "verification", command, result.CommandResult, stdoutArtifactID, stderrArtifactID, now); err != nil {
			return err
		}
		if err := insertVerificationResult(ctx, tx, input.ProjectID, input.RunID, commandEventID, result, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func insertRun(ctx context.Context, tx *sql.Tx, input SaveVerificationInput, status string, now string) error {
	var taskID any
	if input.TaskID != nil {
		taskID = *input.TaskID
	}
	var reverifyContextType any
	if input.ReverifyContextType != "" {
		reverifyContextType = input.ReverifyContextType
	}
	var reverifyContextID any
	if input.ReverifyContextID != "" {
		reverifyContextID = input.ReverifyContextID
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit,
  reverify_context_type, reverify_context_id, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.RunID, input.ProjectID, taskID, input.RunType, status, input.AttemptNo, input.BaseCommit,
		reverifyContextType, reverifyContextID, now, now, now, now,
	)
	return err
}

func (db *DB) saveCommandOutputArtifacts(ctx context.Context, tx *sql.Tx, projectID string, runID string, commandEventID string, commandID string, environmentID string, result runners.RunCommandResult, now string) (*string, *string, error) {
	var stdoutArtifactID *string
	if result.Stdout != "" {
		record, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
			ProjectID:      projectID,
			RunID:          runID,
			CommandEventID: &commandEventID,
			ArtifactType:   "command_stdout",
			ArtifactKey:    commandID + "." + environmentID + ".stdout.txt",
			Content:        []byte(result.Stdout),
		}, now)
		if err != nil {
			return nil, nil, err
		}
		stdoutArtifactID = &record.ID
	}
	var stderrArtifactID *string
	if result.Stderr != "" {
		record, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
			ProjectID:      projectID,
			RunID:          runID,
			CommandEventID: &commandEventID,
			ArtifactType:   "command_stderr",
			ArtifactKey:    commandID + "." + environmentID + ".stderr.txt",
			Content:        []byte(result.Stderr),
		}, now)
		if err != nil {
			return nil, nil, err
		}
		stderrArtifactID = &record.ID
	}
	return stdoutArtifactID, stderrArtifactID, nil
}

func insertCommandEvent(ctx context.Context, tx *sql.Tx, projectID string, runID string, commandEventID string, commandKind string, command verifier.Command, result runners.RunCommandResult, stdoutArtifactID *string, stderrArtifactID *string, now string) error {
	argv, err := json.Marshal(command.Argv)
	if err != nil {
		return err
	}
	detectedRisks, err := json.Marshal(result.DetectedRisks)
	if err != nil {
		return err
	}
	if commandKind == "" {
		commandKind = "verification"
	}
	status := string(result.Status)
	if status == "" {
		status = "failed"
	}
	startedAt := result.StartedAt.Format(time.RFC3339Nano)
	completedAt := result.CompletedAt.Format(time.RFC3339Nano)
	if result.StartedAt.IsZero() {
		startedAt = now
	}
	if result.CompletedAt.IsZero() {
		completedAt = now
	}
	var stdoutValue any
	if stdoutArtifactID != nil {
		stdoutValue = *stdoutArtifactID
	}
	var stderrValue any
	if stderrArtifactID != nil {
		stderrValue = *stderrArtifactID
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO command_events(
  id, project_id, run_id, environment_id, command_kind, runner, cwd, argv_json,
  shell_invocation, network_policy, exit_code, status, detected_risks_json,
  stdout_artifact_id, stderr_artifact_id, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		commandEventID, projectID, runID, command.EnvironmentID, commandKind, command.Runner, command.WorkingDir,
		string(argv), boolInt(command.Runner != "direct"), command.NetworkPolicy, result.ExitCode, status,
		string(detectedRisks), stdoutValue, stderrValue, now, now, startedAt, completedAt,
	)
	return err
}

func insertVerificationResult(ctx context.Context, tx *sql.Tx, projectID string, runID string, commandEventID string, result verifier.Result, now string) error {
	var failureClass any
	if result.FailureClass != nil {
		failureClass = string(*result.FailureClass)
	}
	evidence, err := json.Marshal(map[string]any{
		"message": result.Message,
	})
	if err != nil {
		return err
	}
	resultID := verificationResultID(runID, result.CommandID, result.EnvironmentID)
	_, err = tx.ExecContext(ctx, `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_event_id, command_id,
  required_for_merge, status, failure_class, evidence_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		resultID, projectID, runID, result.EnvironmentID, commandEventID, result.CommandID,
		boolInt(result.RequiredForMerge), result.Status, failureClass, string(evidence), now,
	)
	return err
}

func commandEventID(runID string, commandID string, environmentID string) string {
	return "CMDEVT-" + stableShortHash(runID+"|"+commandID+"|"+environmentID)
}

func verificationResultID(runID string, commandID string, environmentID string) string {
	return "VERIF-" + stableShortHash(runID+"|"+commandID+"|"+environmentID)
}

func stableShortHash(input string) string {
	return strings.ToUpper(ProjectIDForRoot(input)[8:])
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
