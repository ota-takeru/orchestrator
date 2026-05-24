package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ChangeRequestRecord struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ChangeRequestCreateResult struct {
	ChangeRequest ChangeRequestRecord `json:"change_request"`
	QueueItem     WorkQueueItemRecord `json:"queue_item"`
}

func (db *DB) CreateChangeRequest(ctx context.Context, projectID string, text string) (ChangeRequestCreateResult, error) {
	body := strings.TrimSpace(text)
	if strings.TrimSpace(projectID) == "" {
		return ChangeRequestCreateResult{}, fmt.Errorf("project id is required")
	}
	if body == "" {
		return ChangeRequestCreateResult{}, fmt.Errorf("change request text is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	requestID := "CR-" + shortID(projectID, body, now)
	queueID := "WQ-" + shortID(projectID, requestID, now)
	idempotencyKey := "change_request:" + requestID

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ChangeRequestCreateResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO change_requests(
  id, project_id, status, body, created_at, updated_at
) VALUES (?, ?, 'proposed', ?, ?, ?)`,
		requestID, projectID, body, now, now,
	); err != nil {
		return ChangeRequestCreateResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, created_at, updated_at
) VALUES (?, ?, 'planning', 'change_request_analysis', ?, 'queued', 'medium', 0, 3, ?, ?, ?)`,
		queueID, projectID, requestID, idempotencyKey, now, now,
	); err != nil {
		return ChangeRequestCreateResult{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "change_request_proposed", map[string]any{
		"change_request_id":  requestID,
		"work_queue_item_id": queueID,
	}, now); err != nil {
		return ChangeRequestCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChangeRequestCreateResult{}, err
	}
	committed = true
	return ChangeRequestCreateResult{
		ChangeRequest: ChangeRequestRecord{
			ID:        requestID,
			Status:    "proposed",
			Body:      body,
			CreatedAt: now,
			UpdatedAt: now,
		},
		QueueItem: WorkQueueItemRecord{
			ID:             queueID,
			Lane:           "planning",
			ItemType:       "change_request_analysis",
			ItemID:         requestID,
			Status:         "queued",
			Priority:       "medium",
			AttemptNo:      0,
			MaxAttempts:    3,
			IdempotencyKey: idempotencyKey,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}, nil
}
