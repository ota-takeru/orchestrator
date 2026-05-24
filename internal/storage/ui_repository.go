package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type HumanInboxSnapshot struct {
	ProjectID               string             `json:"project_id"`
	GeneratedAt             string             `json:"generated_at"`
	Counts                  HumanInboxUICounts `json:"counts"`
	LastSuccessfulMergeAt   string             `json:"last_successful_merge_at,omitempty"`
	OpenInboxItems          []InboxItem        `json:"open_inbox_items"`
	RecommendedNextCommands []string           `json:"recommended_next_commands,omitempty"`
}

type HumanInboxUICounts struct {
	OpenInboxItems       int `json:"open_inbox_items"`
	RunningTasks         int `json:"running_tasks"`
	WaitingForHumanTasks int `json:"waiting_for_human_tasks"`
	BlockedTasks         int `json:"blocked_tasks"`
	QueuedRequests       int `json:"queued_requests"`
	OpenDecisions        int `json:"open_decisions"`
	RunningWorkers       int `json:"running_workers"`
	OpenMergeQueue       int `json:"open_merge_queue"`
}

func (db *DB) LoadHumanInboxSnapshot(ctx context.Context, projectID string, limit int) (HumanInboxSnapshot, error) {
	if projectID == "" {
		return HumanInboxSnapshot{}, fmt.Errorf("project id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	items, err := db.ListInboxItems(ctx, projectID, "open")
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	snapshot := HumanInboxSnapshot{
		ProjectID:      projectID,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		OpenInboxItems: items,
	}
	snapshot.Counts.OpenInboxItems, err = db.countWhere(ctx, "inbox_items", "project_id = ? AND status = 'open'", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.RunningTasks, err = db.countWhere(ctx, "tasks", "project_id = ? AND status IN ('implementing', 'verifying', 'diagnosing', 'repairing', 'reviewing', 'rebasing', 'reverifying')", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.WaitingForHumanTasks, err = db.countWhere(ctx, "tasks", "project_id = ? AND status IN ('needs_input', 'needs_decision', 'ready_for_human_review', 'merge_conflict')", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.BlockedTasks, err = db.countWhere(ctx, "tasks", "project_id = ? AND status IN ('blocked_on_environment', 'blocked_on_policy', 'failed')", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.QueuedRequests, err = db.countWhere(ctx, "feature_requests", "project_id = ? AND status IN ('queued', 'analyzing', 'running', 'waiting_for_human')", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.OpenDecisions, err = db.countWhere(ctx, "decisions", "project_id = ? AND status = 'open'", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.RunningWorkers, err = db.countWhere(ctx, "worker_runs", "project_id = ? AND status = 'running'", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.Counts.OpenMergeQueue, err = db.countWhere(ctx, "merge_queue_entries", "project_id = ? AND status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict')", projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.LastSuccessfulMergeAt, err = db.lastSuccessfulMergeAt(ctx, projectID)
	if err != nil {
		return HumanInboxSnapshot{}, err
	}
	snapshot.RecommendedNextCommands = recommendedUICommands(snapshot)
	return snapshot, nil
}

func (db *DB) countWhere(ctx context.Context, table string, where string, args ...any) (int, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) lastSuccessfulMergeAt(ctx context.Context, projectID string) (string, error) {
	var value string
	err := db.sql.QueryRowContext(ctx, `
SELECT COALESCE(completed_at, updated_at)
FROM merge_queue_entries
WHERE project_id = ? AND status = 'merged'
ORDER BY COALESCE(completed_at, updated_at) DESC
	LIMIT 1`, projectID).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func recommendedUICommands(snapshot HumanInboxSnapshot) []string {
	var commands []string
	if snapshot.Counts.OpenInboxItems > 0 {
		commands = append(commands, "devos inbox --status open --json")
	}
	if snapshot.Counts.QueuedRequests > 0 || snapshot.Counts.RunningWorkers > 0 {
		commands = append(commands, "devos work status --json")
	}
	if snapshot.Counts.OpenMergeQueue > 0 {
		commands = append(commands, "devos merge queue --json")
	}
	if len(commands) == 0 {
		commands = append(commands, "devos request --json <TEXT>")
	}
	return commands
}
