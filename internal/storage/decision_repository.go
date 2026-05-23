package storage

import (
	"context"
	"database/sql"
	"strings"
)

type DecisionRecord struct {
	ID        string  `json:"id"`
	TaskID    *string `json:"task_id,omitempty"`
	Status    string  `json:"status"`
	Title     string  `json:"title"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func (db *DB) ListDecisions(ctx context.Context, projectID string, status string) ([]DecisionRecord, error) {
	query := `
SELECT id, task_id, status, title, created_at, updated_at
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
		if err := rows.Scan(&decision.ID, &taskID, &decision.Status, &decision.Title, &decision.CreatedAt, &decision.UpdatedAt); err != nil {
			return nil, err
		}
		if taskID.Valid {
			decision.TaskID = &taskID.String
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}
