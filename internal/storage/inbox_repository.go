package storage

import (
	"context"
	"database/sql"
	"fmt"
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

func ValidateInboxStatus(status string) error {
	switch status {
	case "", "open", "snoozed", "resolved", "dismissed":
		return nil
	default:
		return fmt.Errorf("invalid inbox status: %s", status)
	}
}
