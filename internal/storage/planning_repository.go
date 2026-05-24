package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PlanningRunRecord struct {
	ID                   string  `json:"id"`
	FeatureRequestID     *string `json:"feature_request_id,omitempty"`
	ChangeRequestID      *string `json:"change_request_id,omitempty"`
	RunType              string  `json:"run_type"`
	Status               string  `json:"status"`
	ArtifactSnapshotJSON string  `json:"artifact_snapshot_json"`
	InputHash            string  `json:"input_hash"`
	OutputSummary        string  `json:"output_summary,omitempty"`
	StartedAt            string  `json:"started_at,omitempty"`
	FinishedAt           string  `json:"finished_at,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type PlanningArtifactRecord struct {
	ID                   string  `json:"id"`
	PlanningRunID        string  `json:"planning_run_id"`
	FeatureRequestID     *string `json:"feature_request_id,omitempty"`
	ChangeRequestID      *string `json:"change_request_id,omitempty"`
	ArtifactType         string  `json:"artifact_type"`
	Status               string  `json:"status"`
	Path                 string  `json:"path"`
	ContentHash          string  `json:"content_hash"`
	ArtifactSnapshotJSON string  `json:"artifact_snapshot_json"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type PlanStartInput struct {
	ProjectID   string
	Concurrency int
}

type PlanStartResult struct {
	StartedRuns []PlanningRunRecord      `json:"started_runs"`
	Artifacts   []PlanningArtifactRecord `json:"artifacts"`
	QueueItems  []WorkQueueItemRecord    `json:"queue_items"`
}

type PlanningStatus struct {
	Runs      []PlanningRunRecord      `json:"runs"`
	Artifacts []PlanningArtifactRecord `json:"artifacts"`
	Queue     []WorkQueueItemRecord    `json:"queue"`
}

type TaskGroupRecord struct {
	ID               string  `json:"id"`
	FeatureRequestID *string `json:"feature_request_id,omitempty"`
	ChangeRequestID  *string `json:"change_request_id,omitempty"`
	Status           string  `json:"status"`
	Title            string  `json:"title"`
	PlanningUnit     string  `json:"planning_unit"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type PlanConsolidateResult struct {
	TaskGroups        []TaskGroupRecord        `json:"task_groups"`
	AcceptedArtifacts []PlanningArtifactRecord `json:"accepted_artifacts"`
}

type planningQueueCandidate struct {
	QueueItem      WorkQueueItemRecord
	FeatureRequest FeatureRequestRecord
}

type planningConsolidationCandidate struct {
	Artifact       PlanningArtifactRecord
	FeatureRequest FeatureRequestRecord
}

func (db *DB) StartPlanning(ctx context.Context, input PlanStartInput) (PlanStartResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return PlanStartResult{}, fmt.Errorf("project id is required")
	}
	limit := input.Concurrency
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}
	candidates, err := db.listPlanningCandidates(ctx, input.ProjectID, limit)
	if err != nil {
		return PlanStartResult{}, err
	}
	result := PlanStartResult{
		StartedRuns: make([]PlanningRunRecord, 0, len(candidates)),
		Artifacts:   make([]PlanningArtifactRecord, 0, len(candidates)),
		QueueItems:  make([]WorkQueueItemRecord, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		run, artifact, queueItem, err := db.completeFeatureDetailPlanning(ctx, input.ProjectID, candidate)
		if err != nil {
			return PlanStartResult{}, err
		}
		result.StartedRuns = append(result.StartedRuns, run)
		result.Artifacts = append(result.Artifacts, artifact)
		result.QueueItems = append(result.QueueItems, queueItem)
	}
	return result, nil
}

func (db *DB) GetPlanningStatus(ctx context.Context, projectID string) (PlanningStatus, error) {
	runs, err := db.ListPlanningRuns(ctx, projectID)
	if err != nil {
		return PlanningStatus{}, err
	}
	artifacts, err := db.ListPlanningArtifacts(ctx, projectID)
	if err != nil {
		return PlanningStatus{}, err
	}
	queue, err := db.ListWorkQueueItems(ctx, projectID, "")
	if err != nil {
		return PlanningStatus{}, err
	}
	return PlanningStatus{Runs: runs, Artifacts: artifacts, Queue: queue}, nil
}

func (db *DB) ConsolidatePlanning(ctx context.Context, projectID string) (PlanConsolidateResult, error) {
	if strings.TrimSpace(projectID) == "" {
		return PlanConsolidateResult{}, fmt.Errorf("project id is required")
	}
	candidates, err := db.listPlanningConsolidationCandidates(ctx, projectID)
	if err != nil {
		return PlanConsolidateResult{}, err
	}
	result := PlanConsolidateResult{
		TaskGroups:        make([]TaskGroupRecord, 0, len(candidates)),
		AcceptedArtifacts: make([]PlanningArtifactRecord, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		group, artifact, err := db.consolidatePlanningArtifact(ctx, projectID, candidate)
		if err != nil {
			return PlanConsolidateResult{}, err
		}
		result.TaskGroups = append(result.TaskGroups, group)
		result.AcceptedArtifacts = append(result.AcceptedArtifacts, artifact)
	}
	return result, nil
}

func (db *DB) ListPlanningRuns(ctx context.Context, projectID string) ([]PlanningRunRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, feature_request_id, run_type, status, artifact_snapshot_json,
       input_hash, output_summary, started_at, finished_at, created_at, updated_at,
       change_request_id
FROM planning_runs
WHERE project_id = ?
ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []PlanningRunRecord
	for rows.Next() {
		record, err := scanPlanningRun(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) ListPlanningArtifacts(ctx context.Context, projectID string) ([]PlanningArtifactRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, planning_run_id, feature_request_id, artifact_type, status,
       path, content_hash, artifact_snapshot_json, created_at, updated_at,
       change_request_id
FROM planning_artifacts
WHERE project_id = ?
ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []PlanningArtifactRecord
	for rows.Next() {
		record, err := scanPlanningArtifact(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) listPlanningCandidates(ctx context.Context, projectID string, limit int) ([]planningQueueCandidate, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT wq.id, wq.lane, wq.item_type, wq.item_id, wq.status, wq.priority,
       wq.preferred_environment_id, wq.required_environment_id, wq.run_profile_id,
       wq.blocked_reason, wq.run_after, wq.lease_owner, wq.lease_expires_at,
       wq.last_heartbeat_at, wq.attempt_no, wq.max_attempts, wq.idempotency_key,
       wq.started_at, wq.finished_at, wq.created_at, wq.updated_at,
       fr.id, fr.status, fr.title, fr.description, fr.source, fr.priority,
       fr.change_request_id, fr.task_group_id, fr.created_at, fr.updated_at, fr.resolved_at
FROM work_queue_items wq
JOIN feature_requests fr ON fr.project_id = wq.project_id AND fr.id = wq.item_id
WHERE wq.project_id = ?
  AND wq.lane = 'planning'
  AND wq.item_type = 'feature_request_analysis'
  AND wq.status = 'queued'
ORDER BY wq.created_at ASC
LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []planningQueueCandidate
	for rows.Next() {
		queueItem, featureRequest, err := scanPlanningCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, planningQueueCandidate{QueueItem: queueItem, FeatureRequest: featureRequest})
	}
	return candidates, rows.Err()
}

func (db *DB) listPlanningConsolidationCandidates(ctx context.Context, projectID string) ([]planningConsolidationCandidate, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT pa.id, pa.planning_run_id, pa.feature_request_id, pa.artifact_type, pa.status,
       pa.path, pa.content_hash, pa.artifact_snapshot_json, pa.created_at, pa.updated_at,
       fr.id, fr.status, fr.title, fr.description, fr.source, fr.priority,
       fr.change_request_id, fr.task_group_id, fr.created_at, fr.updated_at, fr.resolved_at
FROM planning_artifacts pa
JOIN feature_requests fr ON fr.project_id = pa.project_id AND fr.id = pa.feature_request_id
WHERE pa.project_id = ?
  AND pa.artifact_type = 'feature_detail_report'
  AND pa.status = 'proposed'
  AND fr.task_group_id IS NULL
ORDER BY pa.created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []planningConsolidationCandidate
	for rows.Next() {
		artifact, request, err := scanPlanningConsolidationCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, planningConsolidationCandidate{Artifact: artifact, FeatureRequest: request})
	}
	return candidates, rows.Err()
}

func (db *DB) completeFeatureDetailPlanning(ctx context.Context, projectID string, candidate planningQueueCandidate) (PlanningRunRecord, PlanningArtifactRecord, WorkQueueItemRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	leaseOwner := "devos-plan-start"
	leaseExpiresAt := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
	inputHash := planningInputHash(candidate.FeatureRequest)
	runID := "PLANRUN-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|feature_detail|"+inputHash)
	artifactID := "PLANART-" + stableShortHash(runID+"|feature_detail_report")
	snapshotJSON, err := planningSnapshotJSON(candidate.FeatureRequest)
	if err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	artifactContent, err := json.MarshalIndent(map[string]any{
		"feature_request_id": candidate.FeatureRequest.ID,
		"title":              candidate.FeatureRequest.Title,
		"description":        candidate.FeatureRequest.Description,
		"source":             candidate.FeatureRequest.Source,
		"priority":           candidate.FeatureRequest.Priority,
		"summary":            "Feature request captured for consolidation.",
		"next_step":          "plan_consolidate",
	}, "", "  ")
	if err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	contentHash := sha256Hex(artifactContent)
	artifactPath := filepath.ToSlash(filepath.Join("planning_artifacts", artifactID+".json"))
	if err := db.writePlanningArtifactFile(artifactPath, artifactContent); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'running',
    lease_owner = ?,
    lease_expires_at = ?,
    last_heartbeat_at = ?,
    attempt_no = attempt_no + 1,
    started_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'queued'`,
		leaseOwner, leaseExpiresAt, now, now, now, projectID, candidate.QueueItem.ID,
	)
	if err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if affected != 1 {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, fmt.Errorf("planning queue item is no longer queued: %s", candidate.QueueItem.ID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_runs(
  id, project_id, feature_request_id, run_type, status, artifact_snapshot_json,
  input_hash, output_summary, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, ?, 'feature_detail', 'succeeded', ?, ?, ?, ?, ?, ?, ?)`,
		runID, projectID, candidate.FeatureRequest.ID, snapshotJSON, inputHash,
		"Feature request captured for consolidation.", now, now, now, now,
	); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_artifacts(
  id, project_id, planning_run_id, feature_request_id, artifact_type, status,
  path, content_hash, artifact_snapshot_json, created_at, updated_at
) VALUES (?, ?, ?, ?, 'feature_detail_report', 'proposed', ?, ?, ?, ?, ?)`,
		artifactID, projectID, runID, candidate.FeatureRequest.ID, artifactPath, contentHash, snapshotJSON, now, now,
	); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE feature_requests
SET status = 'planned', updated_at = ?
WHERE project_id = ? AND id = ?`,
		now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'completed',
    lease_expires_at = NULL,
    last_heartbeat_at = ?,
    finished_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ?`,
		now, now, now, projectID, candidate.QueueItem.ID,
	); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "planning_run_succeeded", map[string]any{
		"planning_run_id":    runID,
		"planning_artifact":  artifactID,
		"feature_request_id": candidate.FeatureRequest.ID,
	}, now); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return PlanningRunRecord{}, PlanningArtifactRecord{}, WorkQueueItemRecord{}, err
	}
	committed = true

	featureRequestID := candidate.FeatureRequest.ID
	return PlanningRunRecord{
			ID:                   runID,
			FeatureRequestID:     &featureRequestID,
			RunType:              "feature_detail",
			Status:               "succeeded",
			ArtifactSnapshotJSON: snapshotJSON,
			InputHash:            inputHash,
			OutputSummary:        "Feature request captured for consolidation.",
			StartedAt:            now,
			FinishedAt:           now,
			CreatedAt:            now,
			UpdatedAt:            now,
		}, PlanningArtifactRecord{
			ID:                   artifactID,
			PlanningRunID:        runID,
			FeatureRequestID:     &featureRequestID,
			ArtifactType:         "feature_detail_report",
			Status:               "proposed",
			Path:                 artifactPath,
			ContentHash:          contentHash,
			ArtifactSnapshotJSON: snapshotJSON,
			CreatedAt:            now,
			UpdatedAt:            now,
		}, WorkQueueItemRecord{
			ID:              candidate.QueueItem.ID,
			Lane:            candidate.QueueItem.Lane,
			ItemType:        candidate.QueueItem.ItemType,
			ItemID:          candidate.QueueItem.ItemID,
			Status:          "completed",
			Priority:        candidate.QueueItem.Priority,
			LeaseOwner:      leaseOwner,
			LastHeartbeatAt: now,
			AttemptNo:       candidate.QueueItem.AttemptNo + 1,
			MaxAttempts:     candidate.QueueItem.MaxAttempts,
			IdempotencyKey:  candidate.QueueItem.IdempotencyKey,
			StartedAt:       now,
			FinishedAt:      now,
			CreatedAt:       candidate.QueueItem.CreatedAt,
			UpdatedAt:       now,
		}, nil
}

func (db *DB) consolidatePlanningArtifact(ctx context.Context, projectID string, candidate planningConsolidationCandidate) (TaskGroupRecord, PlanningArtifactRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	groupID := "TG-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|feature_chunk")
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO task_groups(
  id, project_id, feature_request_id, status, title,
  change_request_id, planning_unit, created_at, updated_at
) VALUES (?, ?, ?, 'proposed', ?, NULL, 'feature_chunk', ?, ?)`,
		groupID, projectID, candidate.FeatureRequest.ID, candidate.FeatureRequest.Title, now, now,
	); err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE feature_requests
SET task_group_id = ?, status = 'planned', updated_at = ?
WHERE project_id = ? AND id = ? AND task_group_id IS NULL`,
		groupID, now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE planning_artifacts
SET status = 'accepted', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'proposed'`,
		now, projectID, candidate.Artifact.ID,
	); err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "planning_consolidated", map[string]any{
		"task_group_id":        groupID,
		"planning_artifact_id": candidate.Artifact.ID,
		"feature_request_id":   candidate.FeatureRequest.ID,
	}, now); err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskGroupRecord{}, PlanningArtifactRecord{}, err
	}
	committed = true

	featureRequestID := candidate.FeatureRequest.ID
	acceptedArtifact := candidate.Artifact
	acceptedArtifact.Status = "accepted"
	acceptedArtifact.UpdatedAt = now
	return TaskGroupRecord{
		ID:               groupID,
		FeatureRequestID: &featureRequestID,
		Status:           "proposed",
		Title:            candidate.FeatureRequest.Title,
		PlanningUnit:     "feature_chunk",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, acceptedArtifact, nil
}

func (db *DB) writePlanningArtifactFile(relPath string, content []byte) error {
	path := filepath.Join(db.DataRoot(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func scanPlanningCandidate(scanner interface {
	Scan(dest ...any) error
}) (WorkQueueItemRecord, FeatureRequestRecord, error) {
	var queue WorkQueueItemRecord
	var request FeatureRequestRecord
	var preferredEnvironmentID, requiredEnvironmentID, runProfileID sql.NullString
	var blockedReason, runAfter, leaseOwner, leaseExpiresAt, lastHeartbeatAt sql.NullString
	var startedAt, finishedAt sql.NullString
	var changeRequestID, taskGroupID, resolvedAt sql.NullString
	if err := scanner.Scan(
		&queue.ID,
		&queue.Lane,
		&queue.ItemType,
		&queue.ItemID,
		&queue.Status,
		&queue.Priority,
		&preferredEnvironmentID,
		&requiredEnvironmentID,
		&runProfileID,
		&blockedReason,
		&runAfter,
		&leaseOwner,
		&leaseExpiresAt,
		&lastHeartbeatAt,
		&queue.AttemptNo,
		&queue.MaxAttempts,
		&queue.IdempotencyKey,
		&startedAt,
		&finishedAt,
		&queue.CreatedAt,
		&queue.UpdatedAt,
		&request.ID,
		&request.Status,
		&request.Title,
		&request.Description,
		&request.Source,
		&request.Priority,
		&changeRequestID,
		&taskGroupID,
		&request.CreatedAt,
		&request.UpdatedAt,
		&resolvedAt,
	); err != nil {
		return WorkQueueItemRecord{}, FeatureRequestRecord{}, err
	}
	applyNullableWorkQueueFields(&queue, preferredEnvironmentID, requiredEnvironmentID, runProfileID, blockedReason, runAfter, leaseOwner, leaseExpiresAt, lastHeartbeatAt, startedAt, finishedAt)
	if changeRequestID.Valid {
		request.ChangeRequestID = &changeRequestID.String
	}
	if taskGroupID.Valid {
		request.TaskGroupID = &taskGroupID.String
	}
	if resolvedAt.Valid {
		request.ResolvedAt = resolvedAt.String
	}
	return queue, request, nil
}

func scanPlanningConsolidationCandidate(scanner interface {
	Scan(dest ...any) error
}) (PlanningArtifactRecord, FeatureRequestRecord, error) {
	var artifact PlanningArtifactRecord
	var request FeatureRequestRecord
	var artifactFeatureRequestID sql.NullString
	var changeRequestID, taskGroupID, resolvedAt sql.NullString
	if err := scanner.Scan(
		&artifact.ID,
		&artifact.PlanningRunID,
		&artifactFeatureRequestID,
		&artifact.ArtifactType,
		&artifact.Status,
		&artifact.Path,
		&artifact.ContentHash,
		&artifact.ArtifactSnapshotJSON,
		&artifact.CreatedAt,
		&artifact.UpdatedAt,
		&request.ID,
		&request.Status,
		&request.Title,
		&request.Description,
		&request.Source,
		&request.Priority,
		&changeRequestID,
		&taskGroupID,
		&request.CreatedAt,
		&request.UpdatedAt,
		&resolvedAt,
	); err != nil {
		return PlanningArtifactRecord{}, FeatureRequestRecord{}, err
	}
	if artifactFeatureRequestID.Valid {
		artifact.FeatureRequestID = &artifactFeatureRequestID.String
	}
	if changeRequestID.Valid {
		request.ChangeRequestID = &changeRequestID.String
	}
	if taskGroupID.Valid {
		request.TaskGroupID = &taskGroupID.String
	}
	if resolvedAt.Valid {
		request.ResolvedAt = resolvedAt.String
	}
	return artifact, request, nil
}

func scanPlanningRun(scanner interface {
	Scan(dest ...any) error
}) (PlanningRunRecord, error) {
	var record PlanningRunRecord
	var featureRequestID, outputSummary, startedAt, finishedAt, changeRequestID sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&featureRequestID,
		&record.RunType,
		&record.Status,
		&record.ArtifactSnapshotJSON,
		&record.InputHash,
		&outputSummary,
		&startedAt,
		&finishedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
		&changeRequestID,
	); err != nil {
		return PlanningRunRecord{}, err
	}
	if featureRequestID.Valid {
		record.FeatureRequestID = &featureRequestID.String
	}
	if changeRequestID.Valid {
		record.ChangeRequestID = &changeRequestID.String
	}
	if outputSummary.Valid {
		record.OutputSummary = outputSummary.String
	}
	if startedAt.Valid {
		record.StartedAt = startedAt.String
	}
	if finishedAt.Valid {
		record.FinishedAt = finishedAt.String
	}
	return record, nil
}

func scanPlanningArtifact(scanner interface {
	Scan(dest ...any) error
}) (PlanningArtifactRecord, error) {
	var record PlanningArtifactRecord
	var featureRequestID, changeRequestID sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.PlanningRunID,
		&featureRequestID,
		&record.ArtifactType,
		&record.Status,
		&record.Path,
		&record.ContentHash,
		&record.ArtifactSnapshotJSON,
		&record.CreatedAt,
		&record.UpdatedAt,
		&changeRequestID,
	); err != nil {
		return PlanningArtifactRecord{}, err
	}
	if featureRequestID.Valid {
		record.FeatureRequestID = &featureRequestID.String
	}
	if changeRequestID.Valid {
		record.ChangeRequestID = &changeRequestID.String
	}
	return record, nil
}

func applyNullableWorkQueueFields(record *WorkQueueItemRecord, preferredEnvironmentID, requiredEnvironmentID, runProfileID, blockedReason, runAfter, leaseOwner, leaseExpiresAt, lastHeartbeatAt, startedAt, finishedAt sql.NullString) {
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
}

func planningSnapshotJSON(request FeatureRequestRecord) (string, error) {
	payload := map[string]any{
		"feature_request_id": request.ID,
		"feature_status":     request.Status,
		"feature_updated_at": request.UpdatedAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func planningInputHash(request FeatureRequestRecord) string {
	return sha256Hex([]byte(strings.Join([]string{
		request.ID,
		request.Title,
		request.Description,
		request.UpdatedAt,
	}, "|")))
}
