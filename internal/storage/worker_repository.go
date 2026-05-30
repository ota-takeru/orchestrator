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
	ImplementationAdapter     string
	PlanningConcurrency       int
	ImplementationConcurrency int
	Until                     string
	CodexExecutor             CodexExecutor
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
	WorkerRun     WorkerRunRecord         `json:"worker_run"`
	Recovery      WorkQueueRecoveryResult `json:"recovery"`
	Planning      PlanStartResult         `json:"planning"`
	Consolidation PlanConsolidateResult   `json:"consolidation"`
	Execution     []ExecutionWorkResult   `json:"execution"`
}

type WorkStatus struct {
	WorkerRuns []WorkerRunRecord `json:"worker_runs"`
	Planning   PlanningStatus    `json:"planning"`
}

type ExecutionWorkResult struct {
	TaskID       string              `json:"task_id"`
	TaskStatus   string              `json:"task_status"`
	QueueItem    WorkQueueItemRecord `json:"queue_item"`
	Run          FakeRunResult       `json:"run,omitempty"`
	RealRun      *RealCodexRunResult `json:"real_run,omitempty"`
	Verification *VerifyTaskResult   `json:"verification,omitempty"`
}

type WorkQueueRecoveryResult struct {
	Recovered []WorkQueueItemRecord `json:"recovered"`
	Failed    []WorkQueueItemRecord `json:"failed"`
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
	implementationAdapter := strings.TrimSpace(input.ImplementationAdapter)
	if implementationAdapter == "" {
		implementationAdapter = "fake"
	}
	planningConcurrency := input.PlanningConcurrency
	if planningConcurrency <= 0 {
		planningConcurrency = 3
	}
	started, err := db.createWorkerRun(ctx, input.ProjectID, "planning", "bounded_parallel", planningConcurrency)
	if err != nil {
		return WorkStartResult{}, err
	}
	recovery, workErr := db.RecoverLostWorkQueueLeases(ctx, input.ProjectID)
	if workErr == nil {
		workErr = db.CompleteStaleExecutionQueueItems(ctx, input.ProjectID)
	}
	var planning PlanStartResult
	if workErr == nil {
		planning, workErr = db.StartPlanning(ctx, PlanStartInput{ProjectID: input.ProjectID, Concurrency: planningConcurrency})
	}
	var consolidation PlanConsolidateResult
	if workErr == nil {
		consolidation, workErr = db.ConsolidatePlanning(ctx, input.ProjectID)
	}
	var execution []ExecutionWorkResult
	if workErr == nil {
		workErr = db.EnsureReadyTasksQueued(ctx, input.ProjectID)
	}
	if workErr == nil {
		switch implementationAdapter {
		case "fake":
			execution, workErr = db.ProcessExecutionQueueFake(ctx, input.ProjectID, 1)
		case "real-codex", "codex":
			execution, workErr = db.ProcessExecutionQueueRealCodex(ctx, input.ProjectID, 1, input.CodexExecutor)
		default:
			workErr = fmt.Errorf("unsupported implementation adapter: %s", implementationAdapter)
		}
	}
	stopReason := "no_ready_work"
	if len(planning.StartedRuns) > 0 || len(consolidation.TaskGroups) > 0 || len(execution) > 0 {
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
	return WorkStartResult{WorkerRun: finished, Recovery: recovery, Planning: planning, Consolidation: consolidation, Execution: execution}, nil
}

func (db *DB) EnsureReadyTasksQueued(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("project id is required")
	}
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
	rows, err := tx.QueryContext(ctx, `
SELECT t.id
FROM tasks t
WHERE t.project_id = ?
  AND t.status = 'ready'
  AND NOT EXISTS (
    SELECT 1
    FROM work_queue_items wq
    WHERE wq.project_id = t.project_id
      AND wq.lane = 'execution'
      AND wq.item_type = 'task_implementation'
      AND wq.item_id = t.id
      AND wq.status IN ('queued', 'leased', 'running', 'heartbeat_lost', 'waiting_for_human', 'blocked')
  )
ORDER BY t.id`, projectID)
	if err != nil {
		return err
	}
	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			_ = rows.Close()
			return err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, taskID := range taskIDs {
		queueID, err := requeueTaskImplementationWorkItem(ctx, tx, projectID, taskID, now)
		if err != nil {
			return err
		}
		if err := insertWorkflowEvent(ctx, tx, projectID, "ready_task_requeued", map[string]any{
			"task_id":            taskID,
			"work_queue_item_id": queueID,
		}, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
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

func (db *DB) ProcessExecutionQueueRealCodex(ctx context.Context, projectID string, limit int, executor CodexExecutor) ([]ExecutionWorkResult, error) {
	if limit <= 0 {
		limit = 1
	}
	items, err := db.listExecutionQueueItems(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	results := make([]ExecutionWorkResult, 0, len(items))
	for _, item := range items {
		claimed, err := db.markWorkQueueItemRunning(ctx, projectID, item.ID, "devos-work-start")
		if err != nil {
			return nil, err
		}
		var run RealCodexRunResult
		switch item.ItemType {
		case "task_implementation":
			run, err = db.RunRealCodexTask(ctx, projectID, item.ItemID, executor)
		case "task_repair":
			run, err = db.RunRealCodexRepairTask(ctx, projectID, item.ItemID, executor)
		default:
			err = fmt.Errorf("unsupported execution queue item type: %s", item.ItemType)
		}
		if err != nil {
			_ = db.markWorkQueueItemFailed(ctx, projectID, item.ID, err)
			return nil, err
		}
		var verification *VerifyTaskResult
		taskStatus := run.TaskStatus
		if run.TaskStatus == "verifying" {
			verifyResult, err := db.VerifyTask(ctx, projectID, item.ItemID, VerifyTaskInput{Adapter: "local"})
			if err != nil {
				_ = db.markWorkQueueItemFailed(ctx, projectID, item.ID, err)
				return nil, err
			}
			verification = &verifyResult
			taskStatus = verifyResult.TaskStatus
		}
		completed, err := db.markWorkQueueItemCompleted(ctx, projectID, item.ID, claimed)
		if err != nil {
			return nil, err
		}
		results = append(results, ExecutionWorkResult{
			TaskID:       item.ItemID,
			TaskStatus:   taskStatus,
			QueueItem:    completed,
			RealRun:      &run,
			Verification: verification,
		})
	}
	return results, nil
}

func (db *DB) ProcessExecutionQueueFake(ctx context.Context, projectID string, limit int) ([]ExecutionWorkResult, error) {
	if limit <= 0 {
		limit = 1
	}
	items, err := db.listExecutionQueueItems(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	results := make([]ExecutionWorkResult, 0, len(items))
	for _, item := range items {
		claimed, err := db.markWorkQueueItemRunning(ctx, projectID, item.ID, "devos-work-start")
		if err != nil {
			return nil, err
		}
		var run FakeRunResult
		switch item.ItemType {
		case "task_implementation":
			run, err = db.RunFakeTask(ctx, projectID, item.ItemID)
		case "task_repair":
			run, err = db.RunFakeRepairTask(ctx, projectID, item.ItemID)
		default:
			err = fmt.Errorf("unsupported execution queue item type: %s", item.ItemType)
		}
		if err != nil {
			_ = db.markWorkQueueItemFailed(ctx, projectID, item.ID, err)
			return nil, err
		}
		completed, err := db.markWorkQueueItemCompleted(ctx, projectID, item.ID, claimed)
		if err != nil {
			return nil, err
		}
		results = append(results, ExecutionWorkResult{
			TaskID:     item.ItemID,
			TaskStatus: run.TaskStatus,
			QueueItem:  completed,
			Run:        run,
		})
	}
	return results, nil
}

func (db *DB) CompleteStaleExecutionQueueItems(ctx context.Context, projectID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.sql.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'completed',
    finished_at = ?,
    updated_at = ?
WHERE project_id = ?
  AND lane = 'execution'
  AND status = 'queued'
  AND item_type IN ('task_implementation', 'task_repair')
  AND EXISTS (
    SELECT 1
    FROM tasks t
    WHERE t.project_id = work_queue_items.project_id
      AND t.id = work_queue_items.item_id
      AND NOT (
        (work_queue_items.item_type = 'task_implementation' AND t.status = 'ready')
        OR (work_queue_items.item_type = 'task_repair' AND t.status = 'repairing')
      )
  )`,
		now, now, projectID,
	)
	return err
}

func (db *DB) RecoverLostWorkQueueLeases(ctx context.Context, projectID string) (WorkQueueRecoveryResult, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, lane, item_type, item_id, status, priority, preferred_environment_id,
       required_environment_id, run_profile_id, blocked_reason, run_after,
       lease_owner, lease_expires_at, last_heartbeat_at, attempt_no, max_attempts,
       idempotency_key, started_at, finished_at, created_at, updated_at
FROM work_queue_items
WHERE project_id = ?
  AND status IN ('leased', 'running')
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at < ?
ORDER BY lease_expires_at ASC`, projectID, now)
	if err != nil {
		return WorkQueueRecoveryResult{}, err
	}
	defer rows.Close()
	var expired []WorkQueueItemRecord
	for rows.Next() {
		item, err := scanWorkQueueItem(rows)
		if err != nil {
			return WorkQueueRecoveryResult{}, err
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		return WorkQueueRecoveryResult{}, err
	}
	result := WorkQueueRecoveryResult{}
	for _, item := range expired {
		recovered, failed, err := db.recoverWorkQueueItem(ctx, projectID, item, now)
		if err != nil {
			return WorkQueueRecoveryResult{}, err
		}
		if recovered.ID != "" {
			result.Recovered = append(result.Recovered, recovered)
		}
		if failed.ID != "" {
			result.Failed = append(result.Failed, failed)
		}
	}
	return result, nil
}

func (db *DB) recoverWorkQueueItem(ctx context.Context, projectID string, item WorkQueueItemRecord, now string) (WorkQueueItemRecord, WorkQueueItemRecord, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return WorkQueueItemRecord{}, WorkQueueItemRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	targetStatus := "queued"
	errorJSON := sql.NullString{}
	if item.AttemptNo >= item.MaxAttempts {
		targetStatus = "failed"
		errorJSON = sql.NullString{String: `{"message":"work queue lease expired after max attempts"}`, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_queue_items
SET status = ?,
    lease_owner = NULL,
    lease_expires_at = NULL,
    error_json = ?,
    updated_at = ?
WHERE project_id = ? AND id = ? AND status IN ('leased', 'running')`,
		targetStatus, errorJSON, now, projectID, item.ID,
	); err != nil {
		return WorkQueueItemRecord{}, WorkQueueItemRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "work_queue_lease_recovered", map[string]any{
		"work_queue_item_id": item.ID,
		"from_status":        item.Status,
		"to_status":          targetStatus,
		"attempt_no":         item.AttemptNo,
		"max_attempts":       item.MaxAttempts,
	}, now); err != nil {
		return WorkQueueItemRecord{}, WorkQueueItemRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkQueueItemRecord{}, WorkQueueItemRecord{}, err
	}
	committed = true
	updated, err := db.getWorkQueueItem(ctx, projectID, item.ID)
	if err != nil {
		return WorkQueueItemRecord{}, WorkQueueItemRecord{}, err
	}
	if targetStatus == "failed" {
		return WorkQueueItemRecord{}, updated, nil
	}
	return updated, WorkQueueItemRecord{}, nil
}

func (db *DB) listExecutionQueueItems(ctx context.Context, projectID string, limit int) ([]WorkQueueItemRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT wq.id, wq.lane, wq.item_type, wq.item_id, wq.status, wq.priority,
       wq.preferred_environment_id, wq.required_environment_id, wq.run_profile_id,
       wq.blocked_reason, wq.run_after, wq.lease_owner, wq.lease_expires_at,
       wq.last_heartbeat_at, wq.attempt_no, wq.max_attempts, wq.idempotency_key,
       wq.started_at, wq.finished_at, wq.created_at, wq.updated_at
FROM work_queue_items wq
JOIN tasks t ON t.project_id = wq.project_id AND t.id = wq.item_id
WHERE wq.project_id = ?
  AND wq.lane = 'execution'
  AND wq.status = 'queued'
  AND (
    (wq.item_type = 'task_implementation' AND t.status = 'ready')
    OR (wq.item_type = 'task_repair' AND t.status = 'repairing')
  )
ORDER BY wq.created_at ASC
LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkQueueItemRecord
	for rows.Next() {
		item, err := scanWorkQueueItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) markWorkQueueItemRunning(ctx context.Context, projectID string, queueID string, leaseOwner string) (WorkQueueItemRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	leaseExpiresAt := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
	result, err := db.sql.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'running',
    lease_owner = ?,
    lease_expires_at = ?,
    last_heartbeat_at = ?,
    attempt_no = attempt_no + 1,
    started_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'queued'`,
		leaseOwner, leaseExpiresAt, now, now, now, projectID, queueID,
	)
	if err != nil {
		return WorkQueueItemRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return WorkQueueItemRecord{}, err
	}
	if affected != 1 {
		return WorkQueueItemRecord{}, fmt.Errorf("work queue item is no longer queued: %s", queueID)
	}
	return db.getWorkQueueItem(ctx, projectID, queueID)
}

func (db *DB) markWorkQueueItemCompleted(ctx context.Context, projectID string, queueID string, previous WorkQueueItemRecord) (WorkQueueItemRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.sql.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'completed',
    lease_expires_at = NULL,
    last_heartbeat_at = ?,
    finished_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'running'`,
		now, now, now, projectID, queueID,
	); err != nil {
		return WorkQueueItemRecord{}, err
	}
	record, err := db.getWorkQueueItem(ctx, projectID, queueID)
	if err != nil {
		return WorkQueueItemRecord{}, err
	}
	if record.StartedAt == "" {
		record.StartedAt = previous.StartedAt
	}
	return record, nil
}

func (db *DB) markWorkQueueItemFailed(ctx context.Context, projectID string, queueID string, cause error) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	errorJSON := fmt.Sprintf(`{"message":%q}`, cause.Error())
	_, err := db.sql.ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'failed',
    error_json = ?,
    lease_expires_at = NULL,
    last_heartbeat_at = ?,
    finished_at = ?,
    updated_at = ?
WHERE project_id = ? AND id = ?`,
		errorJSON, now, now, now, projectID, queueID,
	)
	return err
}

func (db *DB) getWorkQueueItem(ctx context.Context, projectID string, queueID string) (WorkQueueItemRecord, error) {
	row := db.sql.QueryRowContext(ctx, `
SELECT id, lane, item_type, item_id, status, priority, preferred_environment_id,
       required_environment_id, run_profile_id, blocked_reason, run_after,
       lease_owner, lease_expires_at, last_heartbeat_at, attempt_no, max_attempts,
       idempotency_key, started_at, finished_at, created_at, updated_at
FROM work_queue_items
WHERE project_id = ? AND id = ?`, projectID, queueID)
	return scanWorkQueueItem(row)
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
