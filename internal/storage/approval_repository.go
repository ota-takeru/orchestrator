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

type ApprovalType string

const (
	ApprovalFinalReview ApprovalType = "final_review"
	ApprovalMerge       ApprovalType = "merge"
)

type ApprovalInput struct {
	ProjectID    string
	TaskID       string
	ApprovalType ApprovalType
	Notes        string
}

type ApprovalRecord struct {
	ID               string
	TaskStatus       string
	ApprovedForMerge bool
}

type approvalEvidence struct {
	RunID                 string   `json:"run_id"`
	HeadCommit            string   `json:"head_commit"`
	DiffHash              string   `json:"diff_hash"`
	VerificationResultIDs []string `json:"verification_result_ids"`
	GateResultIDs         []string `json:"gate_result_ids"`
}

func (db *DB) ApproveTaskEvidence(ctx context.Context, input ApprovalInput) (ApprovalRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ApprovalRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.TaskID) == "" {
		return ApprovalRecord{}, fmt.Errorf("task id is required")
	}
	if input.ApprovalType != ApprovalFinalReview && input.ApprovalType != ApprovalMerge {
		return ApprovalRecord{}, fmt.Errorf("unsupported approval type: %s", input.ApprovalType)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	taskStatus, err := taskStatusForUpdate(ctx, tx, input.ProjectID, input.TaskID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if taskStatus != "ready_for_human_review" && taskStatus != "approved_for_merge" {
		return ApprovalRecord{}, fmt.Errorf("task %s is not ready for approval: %s", input.TaskID, taskStatus)
	}

	evidence, err := collectApprovalEvidence(ctx, tx, input.ProjectID, input.TaskID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if input.ApprovalType == ApprovalMerge {
		if ok, err := matchingApprovalExists(ctx, tx, input.ProjectID, input.TaskID, ApprovalFinalReview, string(evidenceJSON)); err != nil {
			return ApprovalRecord{}, err
		} else if !ok {
			return ApprovalRecord{}, fmt.Errorf("merge approval requires matching final review approval")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	approvalID := approvalID(input.TaskID, input.ApprovalType, string(evidenceJSON))
	if err := upsertApproval(ctx, tx, approvalID, input, string(evidenceJSON), now); err != nil {
		return ApprovalRecord{}, err
	}

	approvedForMerge := false
	if taskStatus == "ready_for_human_review" {
		if ok, err := bothApprovalsExist(ctx, tx, input.ProjectID, input.TaskID, string(evidenceJSON)); err != nil {
			return ApprovalRecord{}, err
		} else if ok {
			if err := statemachine.Task.ValidateTransition("ready_for_human_review", "approved_for_merge"); err != nil {
				return ApprovalRecord{}, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'approved_for_merge', updated_at = ? WHERE id = ? AND project_id = ?", now, input.TaskID, input.ProjectID); err != nil {
				return ApprovalRecord{}, err
			}
			if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "task_approved_for_merge", map[string]any{
				"task_id":  input.TaskID,
				"evidence": evidence,
			}, now); err != nil {
				return ApprovalRecord{}, err
			}
			taskStatus = "approved_for_merge"
			approvedForMerge = true
		}
	}

	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	committed = true
	return ApprovalRecord{ID: approvalID, TaskStatus: taskStatus, ApprovedForMerge: approvedForMerge}, nil
}

func taskStatusForUpdate(ctx context.Context, tx *sql.Tx, projectID string, taskID string) (string, error) {
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ? AND project_id = ?", taskID, projectID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("task not found: %s", taskID)
		}
		return "", err
	}
	return status, nil
}

func collectApprovalEvidence(ctx context.Context, tx *sql.Tx, projectID string, taskID string) (approvalEvidence, error) {
	var evidence approvalEvidence
	var headCommit, diffHash sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT id, head_commit, diff_hash
FROM runs
WHERE project_id = ? AND task_id = ? AND status = 'succeeded'
ORDER BY created_at DESC
LIMIT 1`, projectID, taskID).Scan(&evidence.RunID, &headCommit, &diffHash); err != nil {
		if err == sql.ErrNoRows {
			return approvalEvidence{}, fmt.Errorf("approval requires a succeeded run for task %s", taskID)
		}
		return approvalEvidence{}, err
	}
	if !headCommit.Valid || strings.TrimSpace(headCommit.String) == "" {
		return approvalEvidence{}, fmt.Errorf("approval requires head_commit evidence")
	}
	if !diffHash.Valid || strings.TrimSpace(diffHash.String) == "" {
		return approvalEvidence{}, fmt.Errorf("approval requires diff_hash evidence")
	}
	evidence.HeadCommit = headCommit.String
	evidence.DiffHash = diffHash.String

	verificationIDs, err := collectIDs(ctx, tx, "SELECT id FROM verification_results WHERE project_id = ? AND run_id = ? ORDER BY id", projectID, evidence.RunID)
	if err != nil {
		return approvalEvidence{}, err
	}
	if len(verificationIDs) == 0 {
		return approvalEvidence{}, fmt.Errorf("approval requires verification result evidence")
	}
	gateIDs, err := collectIDs(ctx, tx, "SELECT id FROM gate_results WHERE project_id = ? AND run_id = ? ORDER BY id", projectID, evidence.RunID)
	if err != nil {
		return approvalEvidence{}, err
	}
	if len(gateIDs) == 0 {
		return approvalEvidence{}, fmt.Errorf("approval requires gate result evidence")
	}
	evidence.VerificationResultIDs = verificationIDs
	evidence.GateResultIDs = gateIDs
	return evidence, nil
}

func collectIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func upsertApproval(ctx context.Context, tx *sql.Tx, approvalID string, input ApprovalInput, evidenceJSON string, now string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO human_approvals(
  id, project_id, task_id, approval_type, status, evidence_json, notes,
  created_at, updated_at, resolved_at
) VALUES (?, ?, ?, ?, 'approved', ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = 'approved',
  evidence_json = excluded.evidence_json,
  notes = excluded.notes,
  updated_at = excluded.updated_at,
  resolved_at = excluded.resolved_at`,
		approvalID, input.ProjectID, input.TaskID, input.ApprovalType, evidenceJSON, input.Notes, now, now, now,
	)
	return err
}

func matchingApprovalExists(ctx context.Context, tx *sql.Tx, projectID string, taskID string, approvalType ApprovalType, evidenceJSON string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM human_approvals
WHERE project_id = ? AND task_id = ? AND approval_type = ? AND status = 'approved' AND evidence_json = ?`,
		projectID, taskID, approvalType, evidenceJSON,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func bothApprovalsExist(ctx context.Context, tx *sql.Tx, projectID string, taskID string, evidenceJSON string) (bool, error) {
	finalReview, err := matchingApprovalExists(ctx, tx, projectID, taskID, ApprovalFinalReview, evidenceJSON)
	if err != nil || !finalReview {
		return false, err
	}
	merge, err := matchingApprovalExists(ctx, tx, projectID, taskID, ApprovalMerge, evidenceJSON)
	if err != nil {
		return false, err
	}
	return merge, nil
}

func approvalID(taskID string, approvalType ApprovalType, evidenceJSON string) string {
	return "APPROVAL-" + stableShortHash(taskID+"|"+string(approvalType)+"|"+evidenceJSON)
}
