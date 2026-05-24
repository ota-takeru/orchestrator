package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type WorkStartInput struct {
	ProjectID                 string
	Mode                      string
	PlanningConcurrency       int
	ImplementationConcurrency int
	Until                     string
}

type WorkerRunRecord struct {
	ID              string `json:"id"`
	Lane            string `json:"lane"`
	Mode            string `json:"mode"`
	MaxConcurrency  int    `json:"max_concurrency"`
	Status          string `json:"status"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at,omitempty"`
	StopReason      string `json:"stop_reason,omitempty"`
	LeaseOwner      string `json:"lease_owner,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
}

type WorkStartResult struct {
	WorkerRun     WorkerRunRecord       `json:"worker_run"`
	Planning      PlanStartResult       `json:"planning"`
	Consolidation PlanConsolidateResult `json:"consolidation"`
}

type WorkStatus struct {
	WorkerRuns []WorkerRunRecord `json:"worker_runs"`
	Planning   PlanningStatus    `json:"planning"`
}

func (db *DB) StartWork(ctx context.Context, input WorkStartInput) (WorkStartResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return WorkStartResult{}, fmt.Errorf("project id is required")
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "sequential"
	}
	if mode != "sequential" {
		return WorkStartResult{}, fmt.Errorf("unsupported work mode: %s", mode)
	}
	if input.ImplementationConcurrency == 0 {
		input.ImplementationConcurrency = 1
	}
	if input.ImplementationConcurrency != 1 {
		return WorkStartResult{}, fmt.Errorf("implementation concurrency must be 1")
	}
	planningConcurrency := input.PlanningConcurrency
	if planningConcurrency <= 0 {
		planningConcurrency = 3
	}
	started, err := db.createWorkerRun(ctx, input.ProjectID, "planning", "bounded_parallel", planningConcurrency)
	if err != nil {
		return WorkStartResult{}, err
	}
	planning, workErr := db.StartPlanning(ctx, PlanStartInput{ProjectID: input.ProjectID, Concurrency: planningConcurrency})
	var consolidation PlanConsolidateResult
	if workErr == nil {
		consolidation, workErr = db.ConsolidatePlanning(ctx, input.ProjectID)
	}
	stopReason := "no_ready_work"
	if len(planning.StartedRuns) > 0 || len(consolidation.TaskGroups) > 0 {
		stopReason = "budget_complete"
	}
	status := "stopped"
	if workErr != nil {
		status = "failed"
		stopReason = workErr.Error()
	}
	finished, finishErr := db.finishWorkerRun(ctx, input.ProjectID, started.ID, status, stopReason)
	if finishErr != nil {
		return WorkStartResult{}, finishErr
	}
	if workErr != nil {
		return WorkStartResult{}, workErr
	}
	return WorkStartResult{WorkerRun: finished, Planning: planning, Consolidation: consolidation}, nil
}

func (db *DB) GetWorkStatus(ctx context.Context, projectID string) (WorkStatus, error) {
	workerRuns, err := db.ListWorkerRuns(ctx, projectID)
	if err != nil {
		return WorkStatus{}, err
	}
	planning, err := db.GetPlanningStatus(ctx, projectID)
	if err != nil {
		return WorkStatus{}, err
	}
	return WorkStatus{WorkerRuns: workerRuns, Planning: planning}, nil
}

func (db *DB) PauseWorkerRun(ctx context.Context, projectID string, workerRunID string) (WorkerRunRecord, error) {
	if strings.TrimSpace(workerRunID) == "" {
		return WorkerRunRecord{}, fmt.Errorf("worker run id is required")
	}
	record, err := db.getWorkerRun(ctx, projectID, workerRunID)
	if err != nil {
		return WorkerRunRecord{}, err
	}
	if record.Status == "paused" {
		return record, nil
	}
	if record.Status != "running" {
		return WorkerRunRecord{}, fmt.Errorf("worker run is not running: %s", record.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.sql.ExecContext(ctx, `
UPDATE worker_runs
SET status = 'paused', stop_reason = 'paused_by_human', last_heartbeat_at = ?
WHERE project_id = ? AND id = ? AND status = 'running'`,
		now, projectID, workerRunID,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	return db.getWorkerRun(ctx, projectID, workerRunID)
}

func (db *DB) ResumeWorkerRun(ctx context.Context, projectID string, workerRunID string) (WorkerRunRecord, error) {
	if strings.TrimSpace(workerRunID) == "" {
		return WorkerRunRecord{}, fmt.Errorf("worker run id is required")
	}
	record, err := db.getWorkerRun(ctx, projectID, workerRunID)
	if err != nil {
		return WorkerRunRecord{}, err
	}
	if record.Status == "running" {
		return record, nil
	}
	if record.Status != "paused" {
		return WorkerRunRecord{}, fmt.Errorf("worker run is not paused: %s", record.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.sql.ExecContext(ctx, `
UPDATE worker_runs
SET status = 'running', stop_reason = NULL, last_heartbeat_at = ?
WHERE project_id = ? AND id = ? AND status = 'paused'`,
		now, projectID, workerRunID,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	return db.getWorkerRun(ctx, projectID, workerRunID)
}

func (db *DB) ListWorkerRuns(ctx context.Context, projectID string) ([]WorkerRunRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, lane, mode, max_concurrency, status, started_at, finished_at,
       stop_reason, lease_owner, last_heartbeat_at
FROM worker_runs
WHERE project_id = ?
ORDER BY started_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []WorkerRunRecord
	for rows.Next() {
		record, err := scanWorkerRun(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) getWorkerRun(ctx context.Context, projectID string, workerRunID string) (WorkerRunRecord, error) {
	var record WorkerRunRecord
	var finishedAt, stopReason, leaseOwner, lastHeartbeatAt sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, lane, mode, max_concurrency, status, started_at, finished_at,
       stop_reason, lease_owner, last_heartbeat_at
FROM worker_runs
WHERE project_id = ? AND id = ?`, projectID, workerRunID).Scan(
		&record.ID,
		&record.Lane,
		&record.Mode,
		&record.MaxConcurrency,
		&record.Status,
		&record.StartedAt,
		&finishedAt,
		&stopReason,
		&leaseOwner,
		&lastHeartbeatAt,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	applyNullableWorkerRunFields(&record, finishedAt, stopReason, leaseOwner, lastHeartbeatAt)
	return record, nil
}

func (db *DB) createWorkerRun(ctx context.Context, projectID string, lane string, mode string, maxConcurrency int) (WorkerRunRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := "WORKER-" + stableShortHash(projectID+"|"+lane+"|"+now)
	leaseOwner := "devos-work-start"
	if _, err := db.sql.ExecContext(ctx, `
INSERT INTO worker_runs(
  id, project_id, lane, mode, max_concurrency, status,
  started_at, lease_owner, last_heartbeat_at
) VALUES (?, ?, ?, ?, ?, 'running', ?, ?, ?)`,
		id, projectID, lane, mode, maxConcurrency, now, leaseOwner, now,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	return WorkerRunRecord{
		ID:              id,
		Lane:            lane,
		Mode:            mode,
		MaxConcurrency:  maxConcurrency,
		Status:          "running",
		StartedAt:       now,
		LeaseOwner:      leaseOwner,
		LastHeartbeatAt: now,
	}, nil
}

func (db *DB) finishWorkerRun(ctx context.Context, projectID string, workerRunID string, status string, stopReason string) (WorkerRunRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.sql.ExecContext(ctx, `
UPDATE worker_runs
SET status = ?, finished_at = ?, stop_reason = ?, last_heartbeat_at = ?
WHERE project_id = ? AND id = ? AND status = 'running'`,
		status, now, stopReason, now, projectID, workerRunID,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	return db.getWorkerRun(ctx, projectID, workerRunID)
}

func scanWorkerRun(scanner interface {
	Scan(dest ...any) error
}) (WorkerRunRecord, error) {
	var record WorkerRunRecord
	var finishedAt, stopReason, leaseOwner, lastHeartbeatAt sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.Lane,
		&record.Mode,
		&record.MaxConcurrency,
		&record.Status,
		&record.StartedAt,
		&finishedAt,
		&stopReason,
		&leaseOwner,
		&lastHeartbeatAt,
	); err != nil {
		return WorkerRunRecord{}, err
	}
	applyNullableWorkerRunFields(&record, finishedAt, stopReason, leaseOwner, lastHeartbeatAt)
	return record, nil
}

func applyNullableWorkerRunFields(record *WorkerRunRecord, finishedAt, stopReason, leaseOwner, lastHeartbeatAt sql.NullString) {
	if finishedAt.Valid {
		record.FinishedAt = finishedAt.String
	}
	if stopReason.Valid {
		record.StopReason = stopReason.String
	}
	if leaseOwner.Valid {
		record.LeaseOwner = leaseOwner.String
	}
	if lastHeartbeatAt.Valid {
		record.LastHeartbeatAt = lastHeartbeatAt.String
	}
}
