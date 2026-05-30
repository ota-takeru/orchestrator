package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/statemachine"
)

type DecisionRecord struct {
	ID             string           `json:"id"`
	TaskID         *string          `json:"task_id,omitempty"`
	Status         string           `json:"status"`
	Title          string           `json:"title"`
	Options        []DecisionOption `json:"options,omitempty"`
	SelectedOption string           `json:"selected_option,omitempty"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
	ResolvedAt     string           `json:"resolved_at,omitempty"`
}

type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type DecisionApprovalInput struct {
	ProjectID  string
	DecisionID string
	Option     string
	Notes      string
	Remember   bool
	Memory     RememberDecisionInput
}

func (db *DB) ListDecisions(ctx context.Context, projectID string, status string) ([]DecisionRecord, error) {
	query := `
SELECT id, task_id, status, title, options_json, selected_option, created_at, updated_at, resolved_at
FROM decisions
WHERE project_id = ?`
	args := []any{projectID}
	if strings.TrimSpace(status) != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at ASC"

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var decisions []DecisionRecord
	for rows.Next() {
		var decision DecisionRecord
		var taskID sql.NullString
		var selectedOption, resolvedAt sql.NullString
		var optionsJSON string
		if err := rows.Scan(
			&decision.ID,
			&taskID,
			&decision.Status,
			&decision.Title,
			&optionsJSON,
			&selectedOption,
			&decision.CreatedAt,
			&decision.UpdatedAt,
			&resolvedAt,
		); err != nil {
			return nil, err
		}
		if taskID.Valid {
			decision.TaskID = &taskID.String
		}
		if selectedOption.Valid {
			decision.SelectedOption = selectedOption.String
		}
		if resolvedAt.Valid {
			decision.ResolvedAt = resolvedAt.String
		}
		decision.Options, err = parseDecisionOptions(optionsJSON)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func parseDecisionOptions(raw string) ([]DecisionOption, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var options []DecisionOption
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil, fmt.Errorf("decision options must be JSON: %w", err)
	}
	return options, nil
}

func (db *DB) ApproveDecision(ctx context.Context, input DecisionApprovalInput) (DecisionRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return DecisionRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.DecisionID) == "" {
		return DecisionRecord{}, fmt.Errorf("decision id is required")
	}
	option := strings.TrimSpace(input.Option)
	if option == "" {
		return DecisionRecord{}, fmt.Errorf("decision option is required")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return DecisionRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var decision DecisionRecord
	var taskID sql.NullString
	var evidenceJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT id, task_id, status, title, evidence_json, created_at, updated_at
FROM decisions
WHERE project_id = ? AND id = ?`, input.ProjectID, input.DecisionID).Scan(
		&decision.ID,
		&taskID,
		&decision.Status,
		&decision.Title,
		&evidenceJSON,
		&decision.CreatedAt,
		&decision.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return DecisionRecord{}, fmt.Errorf("decision not found: %s", input.DecisionID)
		}
		return DecisionRecord{}, err
	}
	if taskID.Valid {
		decision.TaskID = &taskID.String
	}
	if decision.Status != "open" {
		return DecisionRecord{}, fmt.Errorf("decision %s is not open: %s", input.DecisionID, decision.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE decisions
SET status = 'approved', selected_option = ?, updated_at = ?, resolved_at = ?
WHERE project_id = ? AND id = ? AND status = 'open'`,
		option, now, now, input.ProjectID, input.DecisionID,
	); err != nil {
		return DecisionRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND source_type = 'decision' AND source_id = ? AND status = 'open'`,
		now, now, input.ProjectID, input.DecisionID,
	); err != nil {
		return DecisionRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "decision_approved", map[string]any{
		"decision_id":     input.DecisionID,
		"selected_option": option,
		"notes":           strings.TrimSpace(input.Notes),
	}, now); err != nil {
		return DecisionRecord{}, err
	}
	if err := db.recordApprovedDependencyDecision(ctx, tx, input, evidenceJSON, option, now); err != nil {
		return DecisionRecord{}, err
	}
	if option == "promote_task_group_proposal" {
		if err := promoteTaskGroupProposal(ctx, tx, input.ProjectID, input.DecisionID, evidenceJSON, now); err != nil {
			return DecisionRecord{}, err
		}
	}
	if err := rememberApprovedDecision(ctx, tx, input.ProjectID, decision, input, now); err != nil {
		return DecisionRecord{}, err
	}
	if taskID.Valid && option == "retry_after_manual_action" {
		if err := resumeTaskAfterDecisionApproval(ctx, tx, input.ProjectID, taskID.String, input.DecisionID, now); err != nil {
			return DecisionRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DecisionRecord{}, err
	}
	committed = true

	decision.Status = "approved"
	decision.SelectedOption = option
	decision.UpdatedAt = now
	decision.ResolvedAt = now
	return decision, nil
}

func promoteTaskGroupProposal(ctx context.Context, tx *sql.Tx, projectID string, decisionID string, evidenceJSON string, now string) error {
	var evidence struct {
		TaskGroupID string `json:"task_group_id"`
		TaskID      string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		return fmt.Errorf("planning decision evidence must be JSON: %w", err)
	}
	if strings.TrimSpace(evidence.TaskGroupID) == "" || strings.TrimSpace(evidence.TaskID) == "" {
		return fmt.Errorf("planning decision evidence requires task_group_id and task_id")
	}
	if err := statemachine.Task.ValidateTransition("proposed", "ready"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE task_groups
SET status = 'ready', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'proposed'`,
		now, projectID, evidence.TaskGroupID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE tasks
SET status = 'ready', updated_at = ?
WHERE project_id = ? AND id = ? AND task_group_id = ? AND status = 'proposed'`,
		now, projectID, evidence.TaskID, evidence.TaskGroupID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("proposed task not found for planning decision: %s", evidence.TaskID)
	}
	queueID, err := enqueueTaskImplementationWorkItem(ctx, tx, projectID, evidence.TaskID, now)
	if err != nil {
		return err
	}
	return insertWorkflowEvent(ctx, tx, projectID, "task_group_proposal_promoted", map[string]any{
		"decision_id":        decisionID,
		"task_group_id":      evidence.TaskGroupID,
		"task_id":            evidence.TaskID,
		"work_queue_item_id": queueID,
	}, now)
}

func resumeTaskAfterDecisionApproval(ctx context.Context, tx *sql.Tx, projectID string, taskID string, decisionID string, now string) error {
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&status); err != nil {
		return err
	}
	if status != "needs_decision" {
		return nil
	}
	if err := statemachine.Task.ValidateTransition("needs_decision", "ready"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'ready', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'needs_decision'", now, projectID, taskID); err != nil {
		return err
	}
	queueID, err := requeueTaskImplementationWorkItem(ctx, tx, projectID, taskID, now)
	if err != nil {
		return err
	}
	return insertWorkflowEvent(ctx, tx, projectID, "task_resumed_after_decision", map[string]any{
		"task_id":            taskID,
		"decision_id":        decisionID,
		"from_status":        "needs_decision",
		"to_status":          "ready",
		"work_queue_item_id": queueID,
	}, now)
}
