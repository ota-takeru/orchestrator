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

type DecisionDraftRecord struct {
	ID                   string  `json:"id"`
	PlanningRunID        string  `json:"planning_run_id,omitempty"`
	FeatureRequestID     *string `json:"feature_request_id,omitempty"`
	ChangeRequestID      *string `json:"change_request_id,omitempty"`
	DecisionType         string  `json:"decision_type"`
	Status               string  `json:"status"`
	Title                string  `json:"title"`
	BatchKey             string  `json:"batch_key,omitempty"`
	RecommendedOption    string  `json:"recommended_option,omitempty"`
	ContentJSON          string  `json:"content_json"`
	ArtifactSnapshotJSON string  `json:"artifact_snapshot_json"`
	PromotedDecisionID   string  `json:"promoted_decision_id,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type PlanStartInput struct {
	ProjectID   string
	Concurrency int
}

type PlanStartResult struct {
	StartedRuns    []PlanningRunRecord      `json:"started_runs"`
	Artifacts      []PlanningArtifactRecord `json:"artifacts"`
	DecisionDrafts []DecisionDraftRecord    `json:"decision_drafts,omitempty"`
	QueueItems     []WorkQueueItemRecord    `json:"queue_items"`
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
	ProposedTasks     []TaskRecord             `json:"proposed_tasks"`
	AcceptedArtifacts []PlanningArtifactRecord `json:"accepted_artifacts"`
}

type RollingCheckpointInput struct {
	ProjectID string
	TaskID    string
}

type RollingCheckpointResult struct {
	Run      PlanningRunRecord      `json:"run"`
	Artifact PlanningArtifactRecord `json:"artifact"`
	Task     TaskRecord             `json:"task"`
	Snapshot RollingCheckpointData  `json:"snapshot"`
}

type RollingCheckpointData struct {
	Task              RollingCheckpointTask              `json:"task"`
	TaskGroup         *RollingCheckpointTaskGroup        `json:"task_group,omitempty"`
	QueueCounts       []RollingCheckpointCount           `json:"queue_counts"`
	PlanningArtifacts []RollingCheckpointArtifactSummary `json:"planning_artifacts"`
	NextAction        string                             `json:"next_action"`
}

type RollingCheckpointTask struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	Title                string `json:"title"`
	TaskGroupID          string `json:"task_group_id,omitempty"`
	CurrentRunID         string `json:"current_run_id,omitempty"`
	BaseBranch           string `json:"base_branch"`
	HeadBranch           string `json:"head_branch,omitempty"`
	UpdatedAt            string `json:"updated_at"`
	VerificationCommands int    `json:"verification_commands"`
}

type RollingCheckpointTaskGroup struct {
	ID               string  `json:"id"`
	FeatureRequestID *string `json:"feature_request_id,omitempty"`
	ChangeRequestID  *string `json:"change_request_id,omitempty"`
	Status           string  `json:"status"`
	Title            string  `json:"title"`
	PlanningUnit     string  `json:"planning_unit"`
	UpdatedAt        string  `json:"updated_at"`
}

type RollingCheckpointCount struct {
	Lane   string `json:"lane"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type RollingCheckpointArtifactSummary struct {
	ArtifactType string `json:"artifact_type"`
	Status       string `json:"status"`
	Count        int    `json:"count"`
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
		StartedRuns:    make([]PlanningRunRecord, 0, len(candidates)*5),
		Artifacts:      make([]PlanningArtifactRecord, 0, len(candidates)*4),
		DecisionDrafts: make([]DecisionDraftRecord, 0, len(candidates)),
		QueueItems:     make([]WorkQueueItemRecord, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		runs, artifacts, drafts, queueItem, err := db.completeFeaturePlanning(ctx, input.ProjectID, candidate)
		if err != nil {
			return PlanStartResult{}, err
		}
		result.StartedRuns = append(result.StartedRuns, runs...)
		result.Artifacts = append(result.Artifacts, artifacts...)
		result.DecisionDrafts = append(result.DecisionDrafts, drafts...)
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
		ProposedTasks:     make([]TaskRecord, 0, len(candidates)),
		AcceptedArtifacts: make([]PlanningArtifactRecord, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		group, task, artifact, err := db.consolidatePlanningArtifact(ctx, projectID, candidate)
		if err != nil {
			return PlanConsolidateResult{}, err
		}
		if group.ID != "" {
			result.TaskGroups = append(result.TaskGroups, group)
		}
		if task.ID != "" {
			result.ProposedTasks = append(result.ProposedTasks, task)
		}
		result.AcceptedArtifacts = append(result.AcceptedArtifacts, artifact)
	}
	return result, nil
}

func (db *DB) CreateRollingCheckpoint(ctx context.Context, input RollingCheckpointInput) (RollingCheckpointResult, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	taskID := strings.TrimSpace(input.TaskID)
	if projectID == "" {
		return RollingCheckpointResult{}, fmt.Errorf("project id is required")
	}
	if taskID == "" {
		return RollingCheckpointResult{}, fmt.Errorf("task id is required")
	}
	snapshot, task, featureRequestID, changeRequestID, err := db.buildRollingCheckpointData(ctx, projectID, taskID)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	inputHash := sha256Hex(snapshotJSON)
	runID := "PLANRUN-" + stableShortHash(projectID+"|"+taskID+"|rolling_checkpoint|"+inputHash)
	artifactID := "PLANART-" + stableShortHash(runID+"|rolling_checkpoint_report")
	artifactPath := filepath.ToSlash(filepath.Join("planning_artifacts", artifactID+".json"))
	artifactContent, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	contentHash := sha256Hex(artifactContent)
	if err := db.writePlanningArtifactFile(artifactPath, artifactContent); err != nil {
		return RollingCheckpointResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO planning_runs(
  id, project_id, feature_request_id, run_type, status, artifact_snapshot_json,
  input_hash, output_summary, started_at, finished_at, created_at, updated_at,
  change_request_id
) VALUES (?, ?, ?, 'rolling_checkpoint', 'succeeded', ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, projectID, nullableString(featureRequestID), string(snapshotJSON), inputHash,
		"Rolling checkpoint captured for task "+taskID+".", now, now, now, now, nullableString(changeRequestID),
	)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	runInserted, err := result.RowsAffected()
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO planning_artifacts(
  id, project_id, planning_run_id, feature_request_id, artifact_type, status,
  path, content_hash, artifact_snapshot_json, created_at, updated_at,
  change_request_id
) VALUES (?, ?, ?, ?, 'rolling_checkpoint_report', 'proposed', ?, ?, ?, ?, ?, ?)`,
		artifactID, projectID, runID, nullableString(featureRequestID), artifactPath,
		contentHash, string(snapshotJSON), now, now, nullableString(changeRequestID),
	); err != nil {
		return RollingCheckpointResult{}, err
	}
	if runInserted == 1 {
		if err := insertWorkflowEvent(ctx, tx, projectID, "rolling_checkpoint_created", map[string]any{
			"planning_run_id":      runID,
			"planning_artifact_id": artifactID,
			"task_id":              taskID,
		}, now); err != nil {
			return RollingCheckpointResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RollingCheckpointResult{}, err
	}
	committed = true

	run, err := db.getPlanningRun(ctx, projectID, runID)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	artifact, err := db.getPlanningArtifact(ctx, projectID, artifactID)
	if err != nil {
		return RollingCheckpointResult{}, err
	}
	return RollingCheckpointResult{Run: run, Artifact: artifact, Task: task, Snapshot: snapshot}, nil
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

func (db *DB) getPlanningRun(ctx context.Context, projectID string, runID string) (PlanningRunRecord, error) {
	row := db.sql.QueryRowContext(ctx, `
SELECT id, feature_request_id, run_type, status, artifact_snapshot_json,
       input_hash, output_summary, started_at, finished_at, created_at, updated_at,
       change_request_id
FROM planning_runs
WHERE project_id = ? AND id = ?`, projectID, runID)
	return scanPlanningRun(row)
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

func (db *DB) getPlanningArtifact(ctx context.Context, projectID string, artifactID string) (PlanningArtifactRecord, error) {
	row := db.sql.QueryRowContext(ctx, `
SELECT id, planning_run_id, feature_request_id, artifact_type, status,
       path, content_hash, artifact_snapshot_json, created_at, updated_at,
       change_request_id
FROM planning_artifacts
WHERE project_id = ? AND id = ?`, projectID, artifactID)
	return scanPlanningArtifact(row)
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
  AND pa.artifact_type = 'task_group_proposal'
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

func (db *DB) completeFeaturePlanning(ctx context.Context, projectID string, candidate planningQueueCandidate) ([]PlanningRunRecord, []PlanningArtifactRecord, []DecisionDraftRecord, WorkQueueItemRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	leaseOwner := "devos-plan-start"
	leaseExpiresAt := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
	inputHash := planningInputHash(candidate.FeatureRequest)
	snapshotJSON, err := planningSnapshotJSON(candidate.FeatureRequest)
	if err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	type plannedArtifact struct {
		runType      string
		artifactType string
		summary      string
		content      map[string]any
	}
	planned := []plannedArtifact{
		{runType: "feature_detail", artifactType: "feature_detail_report", summary: "Feature request detail captured.", content: map[string]any{
			"feature_request_id": candidate.FeatureRequest.ID,
			"title":              candidate.FeatureRequest.Title,
			"description":        candidate.FeatureRequest.Description,
			"source":             candidate.FeatureRequest.Source,
			"priority":           candidate.FeatureRequest.Priority,
			"summary":            "Feature request captured for consolidation.",
			"next_step":          "impact_analysis",
		}},
		{runType: "impact_analysis", artifactType: "impact_analysis_report", summary: "Impact analysis captured.", content: map[string]any{
			"feature_request_id": candidate.FeatureRequest.ID,
			"affected_artifacts": []string{"prd", "architecture", "roadmap", "task_breakdown"},
			"summary":            "Planning worker did not modify canonical artifacts.",
			"next_step":          "task_group_proposal",
		}},
		{runType: "task_group_proposal", artifactType: "task_group_proposal", summary: "Task group proposal captured.", content: map[string]any{
			"feature_request_id": candidate.FeatureRequest.ID,
			"planning_unit":      "feature_chunk",
			"task_title":         "Implement " + candidate.FeatureRequest.Title,
			"next_step":          "plan_consolidate",
		}},
		{runType: "risk_report", artifactType: "risk_report", summary: "Risk report captured.", content: map[string]any{
			"feature_request_id": candidate.FeatureRequest.ID,
			"dependency_risk":    "unknown",
			"db_schema_risk":     "unknown",
			"auth_risk":          "unknown",
			"privacy_risk":       "unknown",
			"next_step":          "decision_batching",
		}},
	}
	featureRequestID := candidate.FeatureRequest.ID
	runs := make([]PlanningRunRecord, 0, len(planned)+1)
	artifacts := make([]PlanningArtifactRecord, 0, len(planned))
	for _, item := range planned {
		runID := "PLANRUN-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|"+item.runType+"|"+inputHash)
		artifactID := "PLANART-" + stableShortHash(runID+"|"+item.artifactType)
		content, err := json.MarshalIndent(item.content, "", "  ")
		if err != nil {
			return nil, nil, nil, WorkQueueItemRecord{}, err
		}
		artifactPath := filepath.ToSlash(filepath.Join("planning_artifacts", artifactID+".json"))
		if err := db.writePlanningArtifactFile(artifactPath, content); err != nil {
			return nil, nil, nil, WorkQueueItemRecord{}, err
		}
		runs = append(runs, PlanningRunRecord{
			ID:                   runID,
			FeatureRequestID:     &featureRequestID,
			RunType:              item.runType,
			Status:               "succeeded",
			ArtifactSnapshotJSON: snapshotJSON,
			InputHash:            inputHash,
			OutputSummary:        item.summary,
			StartedAt:            now,
			FinishedAt:           now,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
		artifacts = append(artifacts, PlanningArtifactRecord{
			ID:                   artifactID,
			PlanningRunID:        runID,
			FeatureRequestID:     &featureRequestID,
			ArtifactType:         item.artifactType,
			Status:               "proposed",
			Path:                 artifactPath,
			ContentHash:          sha256Hex(content),
			ArtifactSnapshotJSON: snapshotJSON,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	decisionRunID := "PLANRUN-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|decision_draft|"+inputHash)
	decisionContent, err := json.Marshal(map[string]any{
		"why_human_required":     "Confirm scope before promoting proposed task group into canonical work.",
		"impact":                 "Planning output may update PRD, roadmap, and task breakdown after approval.",
		"evidence":               []string{"feature_detail_report", "impact_analysis_report", "risk_report"},
		"after_approval_actions": []string{"batch_with_related_decisions", "promote_task_group_proposal"},
	})
	if err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	runs = append(runs, PlanningRunRecord{
		ID:                   decisionRunID,
		FeatureRequestID:     &featureRequestID,
		RunType:              "decision_draft",
		Status:               "succeeded",
		ArtifactSnapshotJSON: snapshotJSON,
		InputHash:            inputHash,
		OutputSummary:        "Decision report draft captured.",
		StartedAt:            now,
		FinishedAt:           now,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	drafts := []DecisionDraftRecord{{
		ID:                   "DECDRAFT-" + stableShortHash(decisionRunID+"|scope"),
		PlanningRunID:        decisionRunID,
		FeatureRequestID:     &featureRequestID,
		DecisionType:         "scope",
		Status:               "draft",
		Title:                "Confirm scope for " + candidate.FeatureRequest.Title,
		BatchKey:             projectID + ":scope",
		RecommendedOption:    "promote_task_group_proposal",
		ContentJSON:          string(decisionContent),
		ArtifactSnapshotJSON: snapshotJSON,
		CreatedAt:            now,
		UpdatedAt:            now,
	}}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
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
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	if affected != 1 {
		return nil, nil, nil, WorkQueueItemRecord{}, fmt.Errorf("planning queue item is no longer queued: %s", candidate.QueueItem.ID)
	}
	for _, run := range runs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_runs(
  id, project_id, feature_request_id, run_type, status, artifact_snapshot_json,
  input_hash, output_summary, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, ?, ?, 'succeeded', ?, ?, ?, ?, ?, ?, ?)`,
			run.ID, projectID, candidate.FeatureRequest.ID, run.RunType, snapshotJSON, inputHash,
			run.OutputSummary, now, now, now, now,
		); err != nil {
			return nil, nil, nil, WorkQueueItemRecord{}, err
		}
	}
	for _, artifact := range artifacts {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO planning_artifacts(
  id, project_id, planning_run_id, feature_request_id, artifact_type, status,
  path, content_hash, artifact_snapshot_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 'proposed', ?, ?, ?, ?, ?)`,
			artifact.ID, projectID, artifact.PlanningRunID, candidate.FeatureRequest.ID,
			artifact.ArtifactType, artifact.Path, artifact.ContentHash, snapshotJSON, now, now,
		); err != nil {
			return nil, nil, nil, WorkQueueItemRecord{}, err
		}
	}
	for _, draft := range drafts {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO decision_report_drafts(
  id, project_id, planning_run_id, feature_request_id, decision_type, status,
  title, batch_key, recommended_option, content_json, artifact_snapshot_json,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?, ?)`,
			draft.ID, projectID, draft.PlanningRunID, candidate.FeatureRequest.ID,
			draft.DecisionType, draft.Title, draft.BatchKey, draft.RecommendedOption,
			draft.ContentJSON, snapshotJSON, now, now,
		); err != nil {
			return nil, nil, nil, WorkQueueItemRecord{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE feature_requests
SET status = 'planned', updated_at = ?
WHERE project_id = ? AND id = ?`,
		now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
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
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "planning_run_succeeded", map[string]any{
		"planning_run_ids":   planningRunIDs(runs),
		"planning_artifacts": planningArtifactIDs(artifacts),
		"decision_drafts":    decisionDraftIDs(drafts),
		"feature_request_id": candidate.FeatureRequest.ID,
	}, now); err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, WorkQueueItemRecord{}, err
	}
	committed = true

	return runs, artifacts, drafts, WorkQueueItemRecord{
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

func planningRunIDs(runs []PlanningRunRecord) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}

func planningArtifactIDs(artifacts []PlanningArtifactRecord) []string {
	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		ids = append(ids, artifact.ID)
	}
	return ids
}

func decisionDraftIDs(drafts []DecisionDraftRecord) []string {
	ids := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		ids = append(ids, draft.ID)
	}
	return ids
}

func (db *DB) markPlanningArtifactStale(ctx context.Context, projectID string, artifactID string, runID string, featureRequestID string, now string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
UPDATE planning_artifacts
SET status = 'stale', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'proposed'`,
		now, projectID, artifactID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE planning_runs
SET status = 'stale', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'succeeded'`,
		now, projectID, runID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE decision_report_drafts
SET status = 'stale', updated_at = ?
WHERE project_id = ? AND feature_request_id = ?
  AND status IN ('draft', 'batched')`,
		now, projectID, featureRequestID,
	); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "planning_artifact_stale", map[string]any{
		"planning_run_id":      runID,
		"planning_artifact_id": artifactID,
		"feature_request_id":   featureRequestID,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (db *DB) consolidatePlanningArtifact(ctx context.Context, projectID string, candidate planningConsolidationCandidate) (TaskGroupRecord, TaskRecord, PlanningArtifactRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	currentSnapshot, err := planningSnapshotJSON(candidate.FeatureRequest)
	if err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if candidate.Artifact.ArtifactSnapshotJSON != currentSnapshot {
		if err := db.markPlanningArtifactStale(ctx, projectID, candidate.Artifact.ID, candidate.Artifact.PlanningRunID, candidate.FeatureRequest.ID, now); err != nil {
			return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
		}
		stale := candidate.Artifact
		stale.Status = "stale"
		stale.UpdatedAt = now
		return TaskGroupRecord{}, TaskRecord{}, stale, nil
	}
	groupID := "TG-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|feature_chunk")
	taskID := "TASK-" + stableShortHash(projectID+"|"+candidate.FeatureRequest.ID+"|implementation")
	taskTitle := "Implement " + candidate.FeatureRequest.Title
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
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
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks(
  id, project_id, task_group_id, status, title, base_branch,
  verification_commands_json, created_at, updated_at
) VALUES (?, ?, ?, 'proposed', ?, 'main', '[]', ?, ?)`,
		taskID, projectID, groupID, taskTitle, now, now,
	); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE feature_requests
SET task_group_id = ?, status = 'planned', updated_at = ?
WHERE project_id = ? AND id = ? AND task_group_id IS NULL`,
		groupID, now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE planning_artifacts
SET status = 'accepted', updated_at = ?
WHERE project_id = ? AND feature_request_id = ? AND status = 'proposed'
  AND artifact_type IN ('feature_detail_report', 'impact_analysis_report', 'task_group_proposal', 'risk_report')`,
		now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE decision_report_drafts
SET status = 'batched', updated_at = ?
WHERE project_id = ? AND feature_request_id = ? AND status = 'draft'`,
		now, projectID, candidate.FeatureRequest.ID,
	); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	canonicalCommitQueueID := "WQ-" + stableShortHash(projectID+"|canonical_commit|"+candidate.FeatureRequest.ID+"|"+groupID)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, 'consolidation', 'canonical_commit', ?, 'completed', 'medium', 1, 1, ?, ?, ?, ?, ?)`,
		canonicalCommitQueueID, projectID, groupID, "canonical_commit:"+groupID, now, now, now, now,
	); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "planning_consolidated", map[string]any{
		"task_group_id":             groupID,
		"task_id":                   taskID,
		"planning_artifact_id":      candidate.Artifact.ID,
		"feature_request_id":        candidate.FeatureRequest.ID,
		"canonical_commit_queue_id": canonicalCommitQueueID,
	}, now); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskGroupRecord{}, TaskRecord{}, PlanningArtifactRecord{}, err
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
		}, TaskRecord{
			ID:     taskID,
			Status: "proposed",
			Title:  taskTitle,
		}, acceptedArtifact, nil
}

func (db *DB) buildRollingCheckpointData(ctx context.Context, projectID string, taskID string) (RollingCheckpointData, TaskRecord, *string, *string, error) {
	var task RollingCheckpointTask
	var taskGroupID, currentRunID, headBranch sql.NullString
	var verificationCommandsJSON string
	row := db.sql.QueryRowContext(ctx, `
SELECT id, status, title, task_group_id, current_run_id, base_branch, head_branch,
       verification_commands_json, updated_at
FROM tasks
WHERE project_id = ? AND id = ?`, projectID, taskID)
	if err := row.Scan(
		&task.ID,
		&task.Status,
		&task.Title,
		&taskGroupID,
		&currentRunID,
		&task.BaseBranch,
		&headBranch,
		&verificationCommandsJSON,
		&task.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return RollingCheckpointData{}, TaskRecord{}, nil, nil, fmt.Errorf("task not found: %s", taskID)
		}
		return RollingCheckpointData{}, TaskRecord{}, nil, nil, err
	}
	if taskGroupID.Valid {
		task.TaskGroupID = taskGroupID.String
	}
	if currentRunID.Valid {
		task.CurrentRunID = currentRunID.String
	}
	if headBranch.Valid {
		task.HeadBranch = headBranch.String
	}
	var commands []TaskVerificationCommand
	if strings.TrimSpace(verificationCommandsJSON) != "" {
		if err := json.Unmarshal([]byte(verificationCommandsJSON), &commands); err != nil {
			return RollingCheckpointData{}, TaskRecord{}, nil, nil, err
		}
	}
	task.VerificationCommands = len(commands)

	var group *RollingCheckpointTaskGroup
	var featureRequestID, changeRequestID *string
	if task.TaskGroupID != "" {
		loaded, err := db.loadRollingCheckpointTaskGroup(ctx, projectID, task.TaskGroupID)
		if err != nil {
			return RollingCheckpointData{}, TaskRecord{}, nil, nil, err
		}
		group = &loaded
		featureRequestID = loaded.FeatureRequestID
		changeRequestID = loaded.ChangeRequestID
	}
	queueCounts, err := db.listRollingCheckpointQueueCounts(ctx, projectID)
	if err != nil {
		return RollingCheckpointData{}, TaskRecord{}, nil, nil, err
	}
	artifactCounts, err := db.listRollingCheckpointArtifactCounts(ctx, projectID)
	if err != nil {
		return RollingCheckpointData{}, TaskRecord{}, nil, nil, err
	}
	snapshot := RollingCheckpointData{
		Task:              task,
		TaskGroup:         group,
		QueueCounts:       queueCounts,
		PlanningArtifacts: artifactCounts,
		NextAction:        rollingCheckpointNextAction(task.Status),
	}
	return snapshot, TaskRecord{ID: task.ID, Status: task.Status, Title: task.Title, VerificationCommands: commands}, featureRequestID, changeRequestID, nil
}

func (db *DB) loadRollingCheckpointTaskGroup(ctx context.Context, projectID string, taskGroupID string) (RollingCheckpointTaskGroup, error) {
	var group RollingCheckpointTaskGroup
	var featureRequestID, changeRequestID sql.NullString
	row := db.sql.QueryRowContext(ctx, `
SELECT id, feature_request_id, change_request_id, status, title, planning_unit, updated_at
FROM task_groups
WHERE project_id = ? AND id = ?`, projectID, taskGroupID)
	if err := row.Scan(
		&group.ID,
		&featureRequestID,
		&changeRequestID,
		&group.Status,
		&group.Title,
		&group.PlanningUnit,
		&group.UpdatedAt,
	); err != nil {
		return RollingCheckpointTaskGroup{}, err
	}
	if featureRequestID.Valid {
		group.FeatureRequestID = &featureRequestID.String
	}
	if changeRequestID.Valid {
		group.ChangeRequestID = &changeRequestID.String
	}
	return group, nil
}

func (db *DB) listRollingCheckpointQueueCounts(ctx context.Context, projectID string) ([]RollingCheckpointCount, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT lane, status, COUNT(*)
FROM work_queue_items
WHERE project_id = ?
GROUP BY lane, status
ORDER BY lane, status`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []RollingCheckpointCount
	for rows.Next() {
		var count RollingCheckpointCount
		if err := rows.Scan(&count.Lane, &count.Status, &count.Count); err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

func (db *DB) listRollingCheckpointArtifactCounts(ctx context.Context, projectID string) ([]RollingCheckpointArtifactSummary, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT artifact_type, status, COUNT(*)
FROM planning_artifacts
WHERE project_id = ?
  AND artifact_type != 'rolling_checkpoint_report'
GROUP BY artifact_type, status
ORDER BY artifact_type, status`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []RollingCheckpointArtifactSummary
	for rows.Next() {
		var count RollingCheckpointArtifactSummary
		if err := rows.Scan(&count.ArtifactType, &count.Status, &count.Count); err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

func rollingCheckpointNextAction(taskStatus string) string {
	switch taskStatus {
	case "proposed":
		return "review_task_proposal"
	case "ready":
		return "execute_task"
	case "implementing", "verifying", "diagnosing", "repairing", "reviewing", "rebasing", "reverifying":
		return "resume_task_work"
	case "needs_input", "needs_decision", "blocked_on_environment", "blocked_on_policy", "merge_conflict":
		return "human_inbox"
	case "ready_for_human_review":
		return "human_review"
	case "approved_for_merge", "queued_for_merge":
		return "merge_queue"
	case "merged", "applied", "failed", "cancelled":
		return "terminal"
	default:
		return "inspect_task"
	}
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
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
		"title":              request.Title,
		"description":        request.Description,
		"priority":           request.Priority,
		"source":             request.Source,
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
		request.Priority,
		request.Source,
	}, "|")))
}
