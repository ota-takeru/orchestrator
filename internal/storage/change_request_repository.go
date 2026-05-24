package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
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

type ChangeAnalyzeResult struct {
	ChangeRequest ChangeRequestRecord    `json:"change_request"`
	Run           PlanningRunRecord      `json:"run"`
	Artifact      PlanningArtifactRecord `json:"artifact"`
	QueueItem     *WorkQueueItemRecord   `json:"queue_item,omitempty"`
}

type ChangeApproveInput struct {
	ProjectID       string
	ChangeRequestID string
	Option          string
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

func (db *DB) ListChangeRequests(ctx context.Context, projectID string, status string) ([]ChangeRequestRecord, error) {
	query := `
SELECT id, status, body, created_at, updated_at
FROM change_requests
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
	var records []ChangeRequestRecord
	for rows.Next() {
		var record ChangeRequestRecord
		if err := rows.Scan(&record.ID, &record.Status, &record.Body, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) AnalyzeChangeRequest(ctx context.Context, projectID string, changeRequestID string) (ChangeAnalyzeResult, error) {
	if strings.TrimSpace(projectID) == "" {
		return ChangeAnalyzeResult{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(changeRequestID) == "" {
		return ChangeAnalyzeResult{}, fmt.Errorf("change request id is required")
	}
	changeRequest, err := db.getChangeRequest(ctx, projectID, changeRequestID)
	if err != nil {
		return ChangeAnalyzeResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	inputHash := sha256Hex([]byte(changeRequest.ID + "|" + changeRequest.Body + "|" + changeRequest.UpdatedAt))
	runID := "PLANRUN-" + stableShortHash(projectID+"|"+changeRequest.ID+"|impact_analysis|"+inputHash)
	artifactID := "PLANART-" + stableShortHash(runID+"|impact_analysis_report")
	snapshotJSON, err := changeRequestSnapshotJSON(changeRequest)
	if err != nil {
		return ChangeAnalyzeResult{}, err
	}
	content, err := json.MarshalIndent(map[string]any{
		"change_request_id": changeRequest.ID,
		"body":              changeRequest.Body,
		"summary":           "Change request captured for impact analysis.",
		"risk":              "unknown",
		"next_step":         "change_approve",
	}, "", "  ")
	if err != nil {
		return ChangeAnalyzeResult{}, err
	}
	contentHash := sha256Hex(content)
	artifactPath := filepath.ToSlash(filepath.Join("planning_artifacts", artifactID+".json"))
	if err := db.writePlanningArtifactFile(artifactPath, content); err != nil {
		return ChangeAnalyzeResult{}, err
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ChangeAnalyzeResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_runs(
  id, project_id, feature_request_id, run_type, status, artifact_snapshot_json,
  input_hash, output_summary, started_at, finished_at, created_at, updated_at,
  change_request_id
) VALUES (?, ?, NULL, 'impact_analysis', 'succeeded', ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, projectID, snapshotJSON, inputHash, "Change request impact analysis captured.", now, now, now, now, changeRequest.ID,
	); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_artifacts(
  id, project_id, planning_run_id, feature_request_id, artifact_type, status,
  path, content_hash, artifact_snapshot_json, created_at, updated_at,
  change_request_id
) VALUES (?, ?, ?, NULL, 'impact_analysis_report', 'proposed', ?, ?, ?, ?, ?, ?)`,
		artifactID, projectID, runID, artifactPath, contentHash, snapshotJSON, now, now, changeRequest.ID,
	); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE change_requests
SET status = 'impact_analyzed', updated_at = ?
WHERE project_id = ? AND id = ?`,
		now, projectID, changeRequest.ID,
	); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if err := insertTraceLink(ctx, tx, projectID, "change_request", changeRequest.ID, "planning_run", runID, "analyzed_by", map[string]any{"run_type": "impact_analysis"}, now); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if err := insertTraceLink(ctx, tx, projectID, "change_request", changeRequest.ID, "planning_artifact", artifactID, "produced", map[string]any{"artifact_type": "impact_analysis_report"}, now); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	queueID, err := completeChangeAnalysisQueueItem(ctx, tx, projectID, changeRequest.ID, now)
	if err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "change_request_impact_analyzed", map[string]any{
		"change_request_id":  changeRequest.ID,
		"planning_run_id":    runID,
		"planning_artifact":  artifactID,
		"work_queue_item_id": queueID,
	}, now); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChangeAnalyzeResult{}, err
	}
	committed = true

	changeRequest.Status = "impact_analyzed"
	changeRequest.UpdatedAt = now
	changeRequestIDCopy := changeRequest.ID
	var queueItem *WorkQueueItemRecord
	if queueID != "" {
		item, err := db.getWorkQueueItem(ctx, projectID, queueID)
		if err != nil {
			return ChangeAnalyzeResult{}, err
		}
		queueItem = &item
	}
	return ChangeAnalyzeResult{
		ChangeRequest: changeRequest,
		Run: PlanningRunRecord{
			ID:                   runID,
			ChangeRequestID:      &changeRequestIDCopy,
			RunType:              "impact_analysis",
			Status:               "succeeded",
			ArtifactSnapshotJSON: snapshotJSON,
			InputHash:            inputHash,
			OutputSummary:        "Change request impact analysis captured.",
			StartedAt:            now,
			FinishedAt:           now,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		Artifact: PlanningArtifactRecord{
			ID:                   artifactID,
			PlanningRunID:        runID,
			ChangeRequestID:      &changeRequestIDCopy,
			ArtifactType:         "impact_analysis_report",
			Status:               "proposed",
			Path:                 artifactPath,
			ContentHash:          contentHash,
			ArtifactSnapshotJSON: snapshotJSON,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		QueueItem: queueItem,
	}, nil
}

func (db *DB) ApproveChangeRequest(ctx context.Context, input ChangeApproveInput) (ChangeRequestRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ChangeRequestRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.ChangeRequestID) == "" {
		return ChangeRequestRecord{}, fmt.Errorf("change request id is required")
	}
	option := strings.TrimSpace(input.Option)
	if option == "" {
		return ChangeRequestRecord{}, fmt.Errorf("change approval option is required")
	}
	record, err := db.getChangeRequest(ctx, input.ProjectID, input.ChangeRequestID)
	if err != nil {
		return ChangeRequestRecord{}, err
	}
	if record.Status == "approved" {
		return ChangeRequestRecord{}, fmt.Errorf("change request is already approved: %s", record.ID)
	}
	if record.Status != "impact_analyzed" {
		return ChangeRequestRecord{}, fmt.Errorf("change request must be impact_analyzed before approval: %s", record.Status)
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ChangeRequestRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE change_requests
SET status = 'approved', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'impact_analyzed'`,
		now, input.ProjectID, record.ID,
	); err != nil {
		return ChangeRequestRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "change_request_approved", map[string]any{
		"change_request_id": record.ID,
		"option":            option,
	}, now); err != nil {
		return ChangeRequestRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChangeRequestRecord{}, err
	}
	committed = true
	record.Status = "approved"
	record.UpdatedAt = now
	return record, nil
}

func (db *DB) getChangeRequest(ctx context.Context, projectID string, changeRequestID string) (ChangeRequestRecord, error) {
	var record ChangeRequestRecord
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, status, body, created_at, updated_at
FROM change_requests
WHERE project_id = ? AND id = ?`, projectID, changeRequestID).Scan(
		&record.ID,
		&record.Status,
		&record.Body,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return ChangeRequestRecord{}, err
	}
	return record, nil
}

func completeChangeAnalysisQueueItem(ctx context.Context, tx *sql.Tx, projectID string, changeRequestID string, now string) (string, error) {
	var queueID string
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM work_queue_items
WHERE project_id = ?
  AND lane = 'planning'
  AND item_type = 'change_request_analysis'
  AND item_id = ?
  AND status IN ('queued', 'leased', 'running')
ORDER BY created_at ASC
LIMIT 1`, projectID, changeRequestID).Scan(&queueID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'completed',
    lease_expires_at = NULL,
    last_heartbeat_at = ?,
    finished_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ?`,
		now, now, now, projectID, queueID,
	)
	return queueID, err
}

func insertTraceLink(ctx context.Context, tx *sql.Tx, projectID string, fromType string, fromID string, toType string, toID string, relation string, evidence map[string]any, now string) error {
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	id := "TRACE-" + stableShortHash(projectID+"|"+fromType+"|"+fromID+"|"+toType+"|"+toID+"|"+relation)
	_, err = tx.ExecContext(ctx, `
INSERT INTO trace_links(
  id, project_id, from_type, from_id, to_type, to_id, relation, evidence_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, fromType, fromID, toType, toID, relation, string(evidenceJSON), now,
	)
	return err
}

func changeRequestSnapshotJSON(request ChangeRequestRecord) (string, error) {
	payload := map[string]any{
		"change_request_id": request.ID,
		"change_status":     request.Status,
		"change_updated_at": request.UpdatedAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
