package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

type CleanupDryRunRecord struct {
	RunID          string                 `json:"run_id"`
	Items          []CleanupPlanItem      `json:"items"`
	WorktreeSafety []WorktreeSafetyRecord `json:"worktree_safety,omitempty"`
}

type CleanupExecuteGuardRecord struct {
	RunID               string                 `json:"run_id"`
	Status              string                 `json:"status"`
	ActualDeleteEnabled bool                   `json:"actual_delete_enabled"`
	Items               []CleanupPlanItem      `json:"items"`
	WorktreeSafety      []WorktreeSafetyRecord `json:"worktree_safety"`
	Blockers            []string               `json:"blockers,omitempty"`
}

type CleanupQuarantineMove struct {
	TaskID         string `json:"task_id"`
	FromPath       string `json:"from_path"`
	QuarantinePath string `json:"quarantine_path"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

type CleanupQuarantineRecord struct {
	RunID               string                  `json:"run_id"`
	Status              string                  `json:"status"`
	ActualDeleteEnabled bool                    `json:"actual_delete_enabled"`
	QuarantineEnabled   bool                    `json:"quarantine_enabled"`
	QuarantineRoot      string                  `json:"quarantine_root"`
	Items               []CleanupPlanItem       `json:"items"`
	WorktreeSafety      []WorktreeSafetyRecord  `json:"worktree_safety"`
	Moves               []CleanupQuarantineMove `json:"moves"`
	Blockers            []string                `json:"blockers,omitempty"`
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
		item.Eligible = len(item.Blockers) == 0
		plan = append(plan, item)
	}
	return plan, rows.Err()
}

func (db *DB) SaveCleanupDryRunEvidence(ctx context.Context, projectID string, plan []CleanupPlanItem) (CleanupDryRunRecord, error) {
	attemptNo, err := db.nextProjectRunAttempt(ctx, projectID, "cleanup")
	if err != nil {
		return CleanupDryRunRecord{}, err
	}
	runID := "RUN-" + stableShortHash(projectID+"|cleanup|"+time.Now().UTC().Format(time.RFC3339Nano))
	status := "succeeded"
	for _, item := range plan {
		if len(item.Blockers) > 0 {
			status = "failed"
			break
		}
	}
	summary, err := json.MarshalIndent(map[string]any{
		"dry_run": true,
		"items":   plan,
		"status":  status,
	}, "", "  ")
	if err != nil {
		return CleanupDryRunRecord{}, err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return CleanupDryRunRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		RunID:      runID,
		RunType:    "cleanup",
		AttemptNo:  attemptNo,
		BaseCommit: "cleanup-dry-run",
	}, status, now); err != nil {
		return CleanupDryRunRecord{}, err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        runID,
		ArtifactType: "summary",
		ArtifactKey:  "cleanup-dry-run-summary.json",
		Content:      summary,
	}, now); err != nil {
		return CleanupDryRunRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "cleanup_dry_run", map[string]any{"run_id": runID, "status": status, "item_count": len(plan)}, now); err != nil {
		return CleanupDryRunRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return CleanupDryRunRecord{}, err
	}
	committed = true
	return CleanupDryRunRecord{RunID: runID, Items: plan}, nil
}

func (db *DB) SaveCleanupExecuteGuardEvidence(ctx context.Context, projectID string, plan []CleanupPlanItem, safety []WorktreeSafetyRecord) (CleanupExecuteGuardRecord, error) {
	attemptNo, err := db.nextProjectRunAttempt(ctx, projectID, "cleanup")
	if err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	blockers := cleanupExecuteBlockers(plan, safety)
	status := "guard_passed"
	if len(blockers) > 0 {
		status = "blocked"
	} else {
		blockers = append(blockers, "actual_delete_not_enabled")
	}
	runID := "RUN-" + stableShortHash(projectID+"|cleanup-execute-guard|"+time.Now().UTC().Format(time.RFC3339Nano))
	summary, err := json.MarshalIndent(map[string]any{
		"execute":               true,
		"status":                status,
		"actual_delete_enabled": false,
		"items":                 plan,
		"worktree_safety":       safety,
		"blockers":              blockers,
	}, "", "  ")
	if err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		RunID:      runID,
		RunType:    "cleanup",
		AttemptNo:  attemptNo,
		BaseCommit: "cleanup-execute-guard",
	}, statusForCleanupGuardRun(status), now); err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        runID,
		ArtifactType: "summary",
		ArtifactKey:  "cleanup-execute-guard-summary.json",
		Content:      summary,
	}, now); err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "cleanup_execute_guard", map[string]any{"run_id": runID, "status": status, "item_count": len(plan), "blockers": blockers}, now); err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return CleanupExecuteGuardRecord{}, err
	}
	committed = true
	return CleanupExecuteGuardRecord{RunID: runID, Status: status, ActualDeleteEnabled: false, Items: plan, WorktreeSafety: safety, Blockers: blockers}, nil
}

func (db *DB) QuarantineCleanupCandidates(ctx context.Context, projectID string, plan []CleanupPlanItem, safety []WorktreeSafetyRecord, quarantineRoot string) (CleanupQuarantineRecord, error) {
	quarantineRoot = strings.TrimSpace(quarantineRoot)
	if quarantineRoot == "" {
		return CleanupQuarantineRecord{}, fmt.Errorf("quarantine root is required")
	}
	attemptNo, err := db.nextProjectRunAttempt(ctx, projectID, "cleanup")
	if err != nil {
		return CleanupQuarantineRecord{}, err
	}
	blockers := cleanupExecuteBlockers(plan, safety)
	moves := []CleanupQuarantineMove{}
	if len(blockers) == 0 {
		if err := os.MkdirAll(quarantineRoot, 0o755); err != nil {
			blockers = append(blockers, "quarantine root is not writable: "+err.Error())
		}
	}
	if len(blockers) == 0 {
		env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
		if err != nil {
			return CleanupQuarantineRecord{}, err
		}
		safetyByTask := map[string]WorktreeSafetyRecord{}
		for _, record := range safety {
			safetyByTask[record.TaskID] = record
		}
		stamp := time.Now().UTC().Format("20060102T150405Z")
		for _, item := range plan {
			if !item.Eligible {
				continue
			}
			record := safetyByTask[item.TaskID]
			destination := filepath.Join(quarantineRoot, item.TaskID+"-"+stamp)
			move := CleanupQuarantineMove{TaskID: item.TaskID, FromPath: record.WorktreePath, QuarantinePath: destination, Status: "quarantined"}
			if err := runGit(ctx, env.ProjectRoot, "worktree", "move", record.WorktreePath, destination); err != nil {
				move.Status = "failed"
				move.Error = err.Error()
				blockers = append(blockers, item.TaskID+": quarantine move failed")
			}
			moves = append(moves, move)
		}
	}
	status := "quarantined"
	if len(blockers) > 0 {
		status = "blocked"
		if len(moves) > 0 {
			status = "partial"
		}
	}
	runID := "RUN-" + stableShortHash(projectID+"|cleanup-quarantine|"+time.Now().UTC().Format(time.RFC3339Nano))
	record := CleanupQuarantineRecord{
		RunID:               runID,
		Status:              status,
		ActualDeleteEnabled: false,
		QuarantineEnabled:   true,
		QuarantineRoot:      quarantineRoot,
		Items:               plan,
		WorktreeSafety:      safety,
		Moves:               moves,
		Blockers:            blockers,
	}
	if err := db.saveCleanupQuarantineEvidence(ctx, projectID, record, attemptNo); err != nil {
		return CleanupQuarantineRecord{}, err
	}
	return record, nil
}

func (db *DB) saveCleanupQuarantineEvidence(ctx context.Context, projectID string, record CleanupQuarantineRecord, attemptNo int) error {
	summary, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
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
	runStatus := "succeeded"
	if record.Status == "blocked" || record.Status == "partial" {
		runStatus = "failed"
	}
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		RunID:      record.RunID,
		RunType:    "cleanup",
		AttemptNo:  attemptNo,
		BaseCommit: "cleanup-quarantine",
	}, runStatus, now); err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        record.RunID,
		ArtifactType: "summary",
		ArtifactKey:  "cleanup-quarantine-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "cleanup_quarantine", map[string]any{
		"run_id":          record.RunID,
		"status":          record.Status,
		"quarantine_root": record.QuarantineRoot,
		"move_count":      len(record.Moves),
		"blockers":        record.Blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func cleanupExecuteBlockers(plan []CleanupPlanItem, safety []WorktreeSafetyRecord) []string {
	blockers := []string{}
	if len(plan) == 0 {
		blockers = append(blockers, "no cleanup candidates")
	}
	safetyByTask := map[string]WorktreeSafetyRecord{}
	for _, record := range safety {
		safetyByTask[record.TaskID] = record
	}
	for _, item := range plan {
		for _, blocker := range item.Blockers {
			blockers = append(blockers, item.TaskID+": "+blocker)
		}
		if item.Eligible {
			if _, ok := safetyByTask[item.TaskID]; !ok {
				blockers = append(blockers, item.TaskID+": worktree safety evidence missing")
			}
		}
	}
	for _, record := range safety {
		if record.Status != "succeeded" {
			for _, blocker := range record.Blockers {
				blockers = append(blockers, record.TaskID+": "+blocker)
			}
			if len(record.Blockers) == 0 {
				blockers = append(blockers, record.TaskID+": worktree safety failed")
			}
		}
	}
	return blockers
}

func statusForCleanupGuardRun(status string) string {
	if status == "guard_passed" {
		return "succeeded"
	}
	return "failed"
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

func (db *DB) nextProjectRunAttempt(ctx context.Context, projectID string, runType string) (int, error) {
	var attempt sqlNullInt64
	if err := db.sql.QueryRowContext(ctx, "SELECT MAX(attempt_no) + 1 FROM runs WHERE project_id = ? AND task_id IS NULL AND run_type = ?", projectID, runType).Scan(&attempt); err != nil {
		return 0, err
	}
	if !attempt.Valid {
		return 1, nil
	}
	return int(attempt.Int64), nil
}

type sqlNullInt64 struct {
	Int64 int64
	Valid bool
}

func (n *sqlNullInt64) Scan(value any) error {
	if value == nil {
		n.Valid = false
		n.Int64 = 0
		return nil
	}
	switch v := value.(type) {
	case int64:
		n.Valid = true
		n.Int64 = v
		return nil
	default:
		return fmt.Errorf("unexpected int64 scan value %T", value)
	}
}
