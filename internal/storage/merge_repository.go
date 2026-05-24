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

type MergeQueueEntry struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`
	BaseCommit string `json:"base_commit"`
	HeadCommit string `json:"head_commit"`
}

type MergeGateStatus struct {
	Queue              []MergeQueueEntry `json:"queue"`
	Blockers           []string          `json:"blockers,omitempty"`
	BlockingInboxItems []InboxItem       `json:"blocking_inbox_items,omitempty"`
	Ready              bool              `json:"ready"`
}

func (db *DB) MergeGateStatus(ctx context.Context, projectID string) (MergeGateStatus, error) {
	queue, err := db.ListMergeQueue(ctx, projectID)
	if err != nil {
		return MergeGateStatus{}, err
	}
	blockers, err := db.unresolvedMergeBlockers(ctx, projectID)
	if err != nil {
		return MergeGateStatus{}, err
	}
	items, err := db.mergeBlockingInboxItems(ctx, projectID)
	if err != nil {
		return MergeGateStatus{}, err
	}
	return MergeGateStatus{
		Queue:              queue,
		Blockers:           blockers,
		BlockingInboxItems: items,
		Ready:              len(blockers) == 0,
	}, nil
}

func (db *DB) PreviewTaskMerge(ctx context.Context, projectID string, taskID string) (MergeQueueEntry, error) {
	if strings.TrimSpace(projectID) == "" {
		return MergeQueueEntry{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return MergeQueueEntry{}, fmt.Errorf("task id is required")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	defer tx.Rollback()
	status, err := taskStatusForUpdate(ctx, tx, projectID, taskID)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	if status != "approved_for_merge" {
		return MergeQueueEntry{}, fmt.Errorf("task %s is not approved for merge: %s", taskID, status)
	}
	_, _, evidence, _, err := mergeApprovalEvidence(ctx, tx, projectID, taskID)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	entryID := "MQ-" + stableShortHash(taskID+"|"+evidence.HeadCommit+"|"+evidence.DiffHash)
	return MergeQueueEntry{ID: entryID, TaskID: taskID, Status: "queued", BaseCommit: evidence.BaseCommit, HeadCommit: evidence.HeadCommit}, nil
}

func (db *DB) QueueTaskForMerge(ctx context.Context, projectID string, taskID string) (MergeQueueEntry, error) {
	if strings.TrimSpace(projectID) == "" {
		return MergeQueueEntry{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return MergeQueueEntry{}, fmt.Errorf("task id is required")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	status, err := taskStatusForUpdate(ctx, tx, projectID, taskID)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	if status != "approved_for_merge" {
		return MergeQueueEntry{}, fmt.Errorf("task %s is not approved for merge: %s", taskID, status)
	}

	finalApprovalID, mergeApprovalID, evidence, evidenceJSON, err := mergeApprovalEvidence(ctx, tx, projectID, taskID)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	entryID := "MQ-" + stableShortHash(taskID+"|"+evidence.HeadCommit+"|"+evidence.DiffHash)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := statemachine.Task.ValidateTransition("approved_for_merge", "queued_for_merge"); err != nil {
		return MergeQueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO merge_queue_entries(
  id, project_id, task_id, status, base_commit, head_commit,
  final_review_approval_id, merge_approval_id, evidence_json, created_at, updated_at
) VALUES (?, ?, ?, 'queued', ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = 'queued',
  evidence_json = excluded.evidence_json,
  updated_at = excluded.updated_at`,
		entryID, projectID, taskID, evidence.BaseCommit, evidence.HeadCommit,
		finalApprovalID, mergeApprovalID, evidenceJSON, now, now,
	); err != nil {
		return MergeQueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'queued_for_merge', updated_at = ? WHERE id = ? AND project_id = ?", now, taskID, projectID); err != nil {
		return MergeQueueEntry{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "task_queued_for_merge", map[string]any{
		"task_id":              taskID,
		"merge_queue_entry_id": entryID,
		"evidence":             evidence,
	}, now); err != nil {
		return MergeQueueEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeQueueEntry{}, err
	}
	committed = true
	return MergeQueueEntry{ID: entryID, TaskID: taskID, Status: "queued", BaseCommit: evidence.BaseCommit, HeadCommit: evidence.HeadCommit}, nil
}

func (db *DB) ListMergeQueue(ctx context.Context, projectID string) ([]MergeQueueEntry, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, task_id, status, base_commit, head_commit
FROM merge_queue_entries
WHERE project_id = ?
ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []MergeQueueEntry
	for rows.Next() {
		var entry MergeQueueEntry
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.Status, &entry.BaseCommit, &entry.HeadCommit); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func mergeApprovalEvidence(ctx context.Context, tx *sql.Tx, projectID string, taskID string) (string, string, approvalEvidence, string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, approval_type, evidence_json
FROM human_approvals
WHERE project_id = ? AND task_id = ? AND status = 'approved'
  AND approval_type IN ('final_review', 'merge')
ORDER BY updated_at DESC`, projectID, taskID)
	if err != nil {
		return "", "", approvalEvidence{}, "", err
	}
	defer rows.Close()

	type approval struct {
		id           string
		approvalType string
		evidenceJSON string
		evidence     approvalEvidence
	}
	var approvals []approval
	for rows.Next() {
		var item approval
		if err := rows.Scan(&item.id, &item.approvalType, &item.evidenceJSON); err != nil {
			return "", "", approvalEvidence{}, "", err
		}
		if err := json.Unmarshal([]byte(item.evidenceJSON), &item.evidence); err != nil {
			return "", "", approvalEvidence{}, "", err
		}
		approvals = append(approvals, item)
	}
	if err := rows.Err(); err != nil {
		return "", "", approvalEvidence{}, "", err
	}
	for _, final := range approvals {
		if final.approvalType != string(ApprovalFinalReview) {
			continue
		}
		for _, merge := range approvals {
			if merge.approvalType == string(ApprovalMerge) && merge.evidenceJSON == final.evidenceJSON {
				return final.id, merge.id, final.evidence, final.evidenceJSON, nil
			}
		}
	}
	return "", "", approvalEvidence{}, "", fmt.Errorf("matching final review and merge approvals not found")
}
