package storage

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type FeatureRequestRecord struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Source          string  `json:"source"`
	Priority        string  `json:"priority"`
	ChangeRequestID *string `json:"change_request_id,omitempty"`
	TaskGroupID     *string `json:"task_group_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	ResolvedAt      string  `json:"resolved_at,omitempty"`
}

type WorkQueueItemRecord struct {
	ID                     string `json:"id"`
	Lane                   string `json:"lane"`
	ItemType               string `json:"item_type"`
	ItemID                 string `json:"item_id"`
	Status                 string `json:"status"`
	Priority               string `json:"priority"`
	PreferredEnvironmentID string `json:"preferred_environment_id,omitempty"`
	RequiredEnvironmentID  string `json:"required_environment_id,omitempty"`
	RunProfileID           string `json:"run_profile_id,omitempty"`
	BlockedReason          string `json:"blocked_reason,omitempty"`
	RunAfter               string `json:"run_after,omitempty"`
	LeaseOwner             string `json:"lease_owner,omitempty"`
	LeaseExpiresAt         string `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt        string `json:"last_heartbeat_at,omitempty"`
	AttemptNo              int    `json:"attempt_no"`
	MaxAttempts            int    `json:"max_attempts"`
	IdempotencyKey         string `json:"idempotency_key"`
	StartedAt              string `json:"started_at,omitempty"`
	FinishedAt             string `json:"finished_at,omitempty"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

type FeatureRequestCreateResult struct {
	FeatureRequest FeatureRequestRecord `json:"feature_request"`
	QueueItem      WorkQueueItemRecord  `json:"queue_item"`
}

func (db *DB) CreateFeatureRequest(ctx context.Context, projectID string, text string) (FeatureRequestCreateResult, error) {
	body := strings.TrimSpace(text)
	if strings.TrimSpace(projectID) == "" {
		return FeatureRequestCreateResult{}, fmt.Errorf("project id is required")
	}
	if body == "" {
		return FeatureRequestCreateResult{}, fmt.Errorf("request text is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	requestID := "FR-" + shortID(projectID, body, now)
	queueID := "WQ-" + shortID(projectID, requestID, now)
	idempotencyKey := "feature_request:" + requestID

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return FeatureRequestCreateResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO feature_requests(
  id, project_id, change_request_id, status, body, title, description,
  source, priority, tier, task_group_id, resolved_at, created_at, updated_at
) VALUES (?, ?, NULL, 'queued', ?, ?, ?, 'human', 'medium', NULL, NULL, NULL, ?, ?)`,
		requestID, projectID, body, body, body, now, now,
	); err != nil {
		return FeatureRequestCreateResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, created_at, updated_at
) VALUES (?, ?, 'planning', 'feature_request_analysis', ?, 'queued', 'medium', 0, 3, ?, ?, ?)`,
		queueID, projectID, requestID, idempotencyKey, now, now,
	); err != nil {
		return FeatureRequestCreateResult{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "feature_request_queued", map[string]any{
		"feature_request_id": requestID,
		"work_queue_item_id": queueID,
		"lane":               "planning",
	}, now); err != nil {
		return FeatureRequestCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FeatureRequestCreateResult{}, err
	}
	committed = true

	return FeatureRequestCreateResult{
		FeatureRequest: FeatureRequestRecord{
			ID:          requestID,
			Status:      "queued",
			Title:       body,
			Description: body,
			Source:      "human",
			Priority:    "medium",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		QueueItem: WorkQueueItemRecord{
			ID:             queueID,
			Lane:           "planning",
			ItemType:       "feature_request_analysis",
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

func (db *DB) ListFeatureRequests(ctx context.Context, projectID string, status string) ([]FeatureRequestRecord, error) {
	query := `
SELECT id, status, title, description, source, priority, change_request_id,
       task_group_id, created_at, updated_at, resolved_at
FROM feature_requests
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

	var records []FeatureRequestRecord
	for rows.Next() {
		record, err := scanFeatureRequest(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) ListWorkQueueItems(ctx context.Context, projectID string, status string) ([]WorkQueueItemRecord, error) {
	query := `
SELECT id, lane, item_type, item_id, status, priority, preferred_environment_id,
       required_environment_id, run_profile_id, blocked_reason, run_after,
       lease_owner, lease_expires_at, last_heartbeat_at, attempt_no, max_attempts,
       idempotency_key, started_at, finished_at, created_at, updated_at
FROM work_queue_items
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

	var records []WorkQueueItemRecord
	for rows.Next() {
		record, err := scanWorkQueueItem(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func scanFeatureRequest(scanner interface {
	Scan(dest ...any) error
}) (FeatureRequestRecord, error) {
	var record FeatureRequestRecord
	var changeRequestID, taskGroupID, resolvedAt sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.Status,
		&record.Title,
		&record.Description,
		&record.Source,
		&record.Priority,
		&changeRequestID,
		&taskGroupID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&resolvedAt,
	); err != nil {
		return FeatureRequestRecord{}, err
	}
	if changeRequestID.Valid {
		record.ChangeRequestID = &changeRequestID.String
	}
	if taskGroupID.Valid {
		record.TaskGroupID = &taskGroupID.String
	}
	if resolvedAt.Valid {
		record.ResolvedAt = resolvedAt.String
	}
	return record, nil
}

func scanWorkQueueItem(scanner interface {
	Scan(dest ...any) error
}) (WorkQueueItemRecord, error) {
	var record WorkQueueItemRecord
	var preferredEnvironmentID, requiredEnvironmentID, runProfileID sql.NullString
	var blockedReason, runAfter, leaseOwner, leaseExpiresAt, lastHeartbeatAt sql.NullString
	var startedAt, finishedAt sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.Lane,
		&record.ItemType,
		&record.ItemID,
		&record.Status,
		&record.Priority,
		&preferredEnvironmentID,
		&requiredEnvironmentID,
		&runProfileID,
		&blockedReason,
		&runAfter,
		&leaseOwner,
		&leaseExpiresAt,
		&lastHeartbeatAt,
		&record.AttemptNo,
		&record.MaxAttempts,
		&record.IdempotencyKey,
		&startedAt,
		&finishedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return WorkQueueItemRecord{}, err
	}
	if preferredEnvironmentID.Valid {
		record.PreferredEnvironmentID = preferredEnvironmentID.String
	}
	if requiredEnvironmentID.Valid {
		record.RequiredEnvironmentID = requiredEnvironmentID.String
	}
	if runProfileID.Valid {
		record.RunProfileID = runProfileID.String
	}
	if blockedReason.Valid {
		record.BlockedReason = blockedReason.String
	}
	if runAfter.Valid {
		record.RunAfter = runAfter.String
	}
	if leaseOwner.Valid {
		record.LeaseOwner = leaseOwner.String
	}
	if leaseExpiresAt.Valid {
		record.LeaseExpiresAt = leaseExpiresAt.String
	}
	if lastHeartbeatAt.Valid {
		record.LastHeartbeatAt = lastHeartbeatAt.String
	}
	if startedAt.Valid {
		record.StartedAt = startedAt.String
	}
	if finishedAt.Valid {
		record.FinishedAt = finishedAt.String
	}
	return record, nil
}

func shortID(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:12])
}
