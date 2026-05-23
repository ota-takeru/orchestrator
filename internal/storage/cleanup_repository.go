package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CleanupPlanOptions struct {
	IncludeMerged    bool
	IncludeApplied   bool
	IncludeCancelled bool
	IncludeFailed    bool
	OlderThan        time.Duration
}

type CleanupPlanItem struct {
	TaskID    string   `json:"task_id"`
	Status    string   `json:"status"`
	Title     string   `json:"title"`
	Eligible  bool     `json:"eligible"`
	Blockers  []string `json:"blockers,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

func (db *DB) BuildCleanupDryRunPlan(ctx context.Context, projectID string, options CleanupPlanOptions) ([]CleanupPlanItem, error) {
	statuses := cleanupStatuses(options)
	args := []any{projectID}
	placeholders := make([]string, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	query := fmt.Sprintf(`
SELECT t.id, t.status, t.title, t.updated_at,
       EXISTS (
         SELECT 1
         FROM run_artifacts ra
         JOIN runs r ON r.id = ra.run_id
         WHERE ra.project_id = t.project_id
           AND r.task_id = t.id
           AND ra.artifact_type = 'diff'
       ) AS has_diff_artifact
FROM tasks t
WHERE t.project_id = ? AND t.status IN (%s)`, strings.Join(placeholders, ","))
	if options.OlderThan > 0 {
		cutoff := time.Now().UTC().Add(-options.OlderThan).Format(time.RFC3339Nano)
		query += " AND updated_at <= ?"
		args = append(args, cutoff)
	}
	query += " ORDER BY updated_at ASC, id ASC"

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plan []CleanupPlanItem
	for rows.Next() {
		var item CleanupPlanItem
		var hasDiffArtifact bool
		if err := rows.Scan(&item.TaskID, &item.Status, &item.Title, &item.UpdatedAt, &hasDiffArtifact); err != nil {
			return nil, err
		}
		if !hasDiffArtifact {
			item.Blockers = append(item.Blockers, "diff artifact is not saved")
		}
		item.Blockers = append(item.Blockers, "worktree deletion is not implemented; dry-run only")
		item.Eligible = len(item.Blockers) == 0
		plan = append(plan, item)
	}
	return plan, rows.Err()
}

func cleanupStatuses(options CleanupPlanOptions) []string {
	if !options.IncludeMerged && !options.IncludeApplied && !options.IncludeCancelled && !options.IncludeFailed {
		return []string{"merged", "applied", "cancelled", "failed"}
	}
	var statuses []string
	if options.IncludeMerged {
		statuses = append(statuses, "merged")
	}
	if options.IncludeApplied {
		statuses = append(statuses, "applied")
	}
	if options.IncludeCancelled {
		statuses = append(statuses, "cancelled")
	}
	if options.IncludeFailed {
		statuses = append(statuses, "failed")
	}
	return statuses
}
