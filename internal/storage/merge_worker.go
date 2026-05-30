package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type FakeMergeResult struct {
	MergeQueueEntryID string `json:"merge_queue_entry_id"`
	TaskID            string `json:"task_id"`
	TaskStatus        string `json:"task_status"`
	ReverifyRunID     string `json:"reverify_run_id"`
}

func (db *DB) ProcessNextFakeMerge(ctx context.Context, projectID string) (FakeMergeResult, error) {
	return db.ProcessFakeMerge(ctx, projectID, "")
}

func (db *DB) ProcessFakeMerge(ctx context.Context, projectID string, entryID string) (FakeMergeResult, error) {
	entry, err := db.nextQueuedMergeEntry(ctx, projectID)
	if entryID != "" {
		entry, err = db.mergeEntryByID(ctx, projectID, entryID, "queued")
	}
	if err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "queued", "rebasing"); err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "rebasing", "reverifying"); err != nil {
		return FakeMergeResult{}, err
	}

	return db.reverifyAndMarkFakeMerge(ctx, projectID, entry)
}

func (db *DB) ProcessNextFakeMergeConflict(ctx context.Context, projectID string, reason string) (FakeMergeResult, error) {
	entry, err := db.nextQueuedMergeEntry(ctx, projectID)
	if err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "queued", "rebasing"); err != nil {
		return FakeMergeResult{}, err
	}
	if reason == "" {
		reason = "fake merge conflict"
	}
	if err := db.markMergeQueueConflict(ctx, projectID, entry.ID, entry.TaskID, reason); err != nil {
		return FakeMergeResult{}, err
	}
	return FakeMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, TaskStatus: "merge_conflict"}, nil
}

func (db *DB) RetryFakeMergeConflict(ctx context.Context, projectID string, entryID string) (FakeMergeResult, error) {
	entry, err := db.mergeEntryByID(ctx, projectID, entryID, "merge_conflict")
	if err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "merge_conflict", "rebasing"); err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "rebasing", "reverifying"); err != nil {
		return FakeMergeResult{}, err
	}
	return db.reverifyAndMarkFakeMerge(ctx, projectID, entry)
}

func (db *DB) CancelMergeConflict(ctx context.Context, projectID string, entryID string) (MergeQueueEntry, error) {
	entry, err := db.mergeEntryByID(ctx, projectID, entryID, "merge_conflict")
	if err != nil {
		return MergeQueueEntry{}, err
	}
	if err := statemachine.MergeQueue.ValidateTransition("merge_conflict", "cancelled"); err != nil {
		return MergeQueueEntry{}, err
	}
	if err := statemachine.Task.ValidateTransition("merge_conflict", "cancelled"); err != nil {
		return MergeQueueEntry{}, err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return MergeQueueEntry{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE merge_queue_entries SET status = 'cancelled', updated_at = ?, completed_at = ? WHERE project_id = ? AND id = ? AND status = 'merge_conflict'", now, now, projectID, entry.ID); err != nil {
		return MergeQueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'cancelled', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'merge_conflict'", now, projectID, entry.TaskID); err != nil {
		return MergeQueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE inbox_items SET status = 'resolved', updated_at = ?, resolved_at = ? WHERE project_id = ? AND source_type = 'merge_conflict' AND source_id = ? AND status = 'open'", now, now, projectID, entry.ID); err != nil {
		return MergeQueueEntry{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "merge_conflict_cancelled", map[string]any{"task_id": entry.TaskID, "merge_queue_entry_id": entry.ID}, now); err != nil {
		return MergeQueueEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeQueueEntry{}, err
	}
	committed = true
	entry.Status = "cancelled"
	return entry, nil
}

func (db *DB) nextQueuedMergeEntry(ctx context.Context, projectID string) (MergeQueueEntry, error) {
	var entry MergeQueueEntry
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, task_id, status, base_commit, head_commit
FROM merge_queue_entries
WHERE project_id = ? AND status = 'queued'
ORDER BY created_at ASC
LIMIT 1`, projectID).Scan(&entry.ID, &entry.TaskID, &entry.Status, &entry.BaseCommit, &entry.HeadCommit); err != nil {
		if err == sql.ErrNoRows {
			return MergeQueueEntry{}, fmt.Errorf("queued merge entry not found")
		}
		return MergeQueueEntry{}, err
	}
	return entry, nil
}

func (db *DB) mergeEntryByID(ctx context.Context, projectID string, entryID string, status string) (MergeQueueEntry, error) {
	var entry MergeQueueEntry
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, task_id, status, base_commit, head_commit
FROM merge_queue_entries
WHERE project_id = ? AND id = ? AND status = ?`, projectID, entryID, status).Scan(&entry.ID, &entry.TaskID, &entry.Status, &entry.BaseCommit, &entry.HeadCommit); err != nil {
		if err == sql.ErrNoRows {
			return MergeQueueEntry{}, fmt.Errorf("merge queue entry not found: %s", entryID)
		}
		return MergeQueueEntry{}, err
	}
	return entry, nil
}

func (db *DB) syncMergeQueueState(ctx context.Context, projectID string, entryID string, taskID string, from string, to string) error {
	if err := statemachine.MergeQueue.ValidateTransition(from, to); err != nil {
		return err
	}
	taskFrom := map[string]string{"queued": "queued_for_merge", "rebasing": "rebasing", "merge_conflict": "merge_conflict"}[from]
	taskTo := map[string]string{"rebasing": "rebasing", "reverifying": "reverifying"}[to]
	if err := statemachine.Task.ValidateTransition(taskFrom, taskTo); err != nil {
		return err
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE merge_queue_entries SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status = ?", to, now, projectID, entryID, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status = ?", taskTo, now, projectID, taskID, taskFrom); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "merge_queue_"+to, map[string]any{"task_id": taskID, "merge_queue_entry_id": entryID}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (db *DB) reverifyAndMarkFakeMerge(ctx context.Context, projectID string, entry MergeQueueEntry) (FakeMergeResult, error) {
	env, err := db.primaryEnvironment(ctx, projectID)
	if err != nil {
		return FakeMergeResult{}, err
	}
	runID := "RUN-" + stableShortHash(entry.TaskID+"|reverify|"+time.Now().UTC().Format(time.RFC3339Nano))
	command := verifier.Command{
		ID:               "fake-reverify",
		EnvironmentID:    env.ID,
		Runner:           "fake",
		WorkingDir:       env.ProjectRoot,
		Argv:             []string{"reverify"},
		NetworkPolicy:    runners.NetworkOff,
		RequiredForMerge: true,
	}
	report, err := verifier.Run(ctx, runID, verifier.StaticRunnerRegistry{env.ID: fakeRunnerForEnvironment(env)}, []verifier.Command{command})
	if err != nil {
		return FakeMergeResult{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:           projectID,
		TaskID:              &entry.TaskID,
		RunID:               runID,
		RunType:             "reverify",
		AttemptNo:           1,
		BaseCommit:          entry.BaseCommit,
		ReverifyContextType: "merge_queue_entry",
		ReverifyContextID:   entry.ID,
		Commands:            []verifier.Command{command},
		Report:              report,
	}); err != nil {
		return FakeMergeResult{}, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &entry.TaskID, runID, gates); err != nil {
		return FakeMergeResult{}, err
	}
	if taskStatusFromGateResults(gates) != "ready_for_human_review" {
		return FakeMergeResult{}, fmt.Errorf("fake merge reverify did not pass")
	}
	if err := db.markMergeQueueMerged(ctx, projectID, entry.ID, entry.TaskID); err != nil {
		return FakeMergeResult{}, err
	}
	return FakeMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, TaskStatus: "merged", ReverifyRunID: runID}, nil
}

func (db *DB) markMergeQueueConflict(ctx context.Context, projectID string, entryID string, taskID string, reason string) error {
	if err := statemachine.MergeQueue.ValidateTransition("rebasing", "merge_conflict"); err != nil {
		return err
	}
	if err := statemachine.Task.ValidateTransition("rebasing", "merge_conflict"); err != nil {
		return err
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE merge_queue_entries SET status = 'merge_conflict', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'rebasing'", now, projectID, entryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'merge_conflict', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'rebasing'", now, projectID, taskID); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "merge_conflict_detected", map[string]any{"task_id": taskID, "merge_queue_entry_id": entryID, "reason": reason}, now); err != nil {
		return err
	}
	inboxID := "INBOX-" + stableShortHash(projectID+"|merge_conflict|"+entryID)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, ?, 'human_decision', 'open', 'merge_conflict', ?, ?, 80, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  updated_at = excluded.updated_at,
  body = excluded.body`,
		inboxID, projectID, taskID, entryID, "merge_conflict:"+entryID,
		"Merge conflict requires decision", reason, now, now,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (db *DB) markMergeQueueMerged(ctx context.Context, projectID string, entryID string, taskID string) error {
	if err := statemachine.MergeQueue.ValidateTransition("reverifying", "merged"); err != nil {
		return err
	}
	if err := statemachine.Task.ValidateTransition("reverifying", "merged"); err != nil {
		return err
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE merge_queue_entries SET status = 'merged', updated_at = ?, completed_at = ? WHERE project_id = ? AND id = ? AND status = 'reverifying'", now, now, projectID, entryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'merged', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'reverifying'", now, projectID, taskID); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "task_merged", map[string]any{"task_id": taskID, "merge_queue_entry_id": entryID}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
