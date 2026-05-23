package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type InboxItem struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	TaskID     *string `json:"task_id,omitempty"`
	ItemType   string  `json:"item_type"`
	Status     string  `json:"status"`
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	Priority   int     `json:"priority"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

type InboxApprovalInput struct {
	ProjectID string
	InboxID   string
	Option    string
	Notes     string
}

type InboxApprovalResult struct {
	InboxID       string          `json:"inbox_id"`
	SourceType    string          `json:"source_type"`
	SourceID      string          `json:"source_id"`
	Decision      *DecisionRecord `json:"decision,omitempty"`
	HumanApproval *ApprovalRecord `json:"human_approval,omitempty"`
}

func (db *DB) ListInboxItems(ctx context.Context, projectID string, status string) ([]InboxItem, error) {
	query := `
SELECT id, project_id, task_id, item_type, status, source_type, source_id,
       priority, title, body, created_at, updated_at
FROM inbox_items
WHERE project_id = ?`
	args := []any{projectID}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY priority DESC, created_at ASC"

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []InboxItem
	for rows.Next() {
		var item InboxItem
		var taskID sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.ProjectID,
			&taskID,
			&item.ItemType,
			&item.Status,
			&item.SourceType,
			&item.SourceID,
			&item.Priority,
			&item.Title,
			&item.Body,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if taskID.Valid {
			item.TaskID = &taskID.String
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (db *DB) ApproveInboxItem(ctx context.Context, input InboxApprovalInput) (InboxApprovalResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return InboxApprovalResult{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.InboxID) == "" {
		return InboxApprovalResult{}, fmt.Errorf("inbox id is required")
	}
	item, err := db.getInboxItem(ctx, input.ProjectID, input.InboxID)
	if err != nil {
		return InboxApprovalResult{}, err
	}
	if item.Status != "open" {
		return InboxApprovalResult{}, fmt.Errorf("inbox item %s is not open: %s", input.InboxID, item.Status)
	}
	result := InboxApprovalResult{InboxID: item.ID, SourceType: item.SourceType, SourceID: item.SourceID}
	switch item.SourceType {
	case "decision":
		decision, err := db.ApproveDecision(ctx, DecisionApprovalInput{
			ProjectID:  input.ProjectID,
			DecisionID: item.SourceID,
			Option:     input.Option,
			Notes:      input.Notes,
		})
		if err != nil {
			return InboxApprovalResult{}, err
		}
		result.Decision = &decision
		return result, nil
	case "human_approval":
		approval, err := db.ApproveHumanApproval(ctx, input.ProjectID, item.SourceID, input.Notes)
		if err != nil {
			return InboxApprovalResult{}, err
		}
		result.HumanApproval = &approval
		return result, nil
	default:
		return InboxApprovalResult{}, fmt.Errorf("inbox source type %s is not supported for approve", item.SourceType)
	}
}

func ValidateInboxStatus(status string) error {
	switch status {
	case "", "open", "snoozed", "resolved", "dismissed":
		return nil
	default:
		return fmt.Errorf("invalid inbox status: %s", status)
	}
}

func (db *DB) GetInboxItem(ctx context.Context, projectID string, inboxID string) (InboxItem, error) {
	return db.getInboxItem(ctx, projectID, inboxID)
}

func (db *DB) getInboxItem(ctx context.Context, projectID string, inboxID string) (InboxItem, error) {
	var item InboxItem
	var taskID sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, project_id, task_id, item_type, status, source_type, source_id,
       priority, title, body, created_at, updated_at
FROM inbox_items
WHERE project_id = ? AND id = ?`, projectID, inboxID).Scan(
		&item.ID,
		&item.ProjectID,
		&taskID,
		&item.ItemType,
		&item.Status,
		&item.SourceType,
		&item.SourceID,
		&item.Priority,
		&item.Title,
		&item.Body,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return InboxItem{}, fmt.Errorf("inbox item not found: %s", inboxID)
		}
		return InboxItem{}, err
	}
	if taskID.Valid {
		item.TaskID = &taskID.String
	}
	return item, nil
}
