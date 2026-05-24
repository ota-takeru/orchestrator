package storage

import (
	"context"
	"database/sql"
	"fmt"
)

type InvariantViolation struct {
	Scope   string `json:"scope"`
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (db *DB) CheckArtifactInvariants(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	var violations []InvariantViolation
	checks := []func(context.Context, string) ([]InvariantViolation, error){
		db.checkApprovedVersionReferences,
		db.checkLatestVersionReferences,
		db.checkApprovedVersionStatuses,
		db.checkApprovedWithNotes,
		db.checkReadyTaskArtifactContext,
	}
	for _, check := range checks {
		found, err := check(ctx, projectID)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	return violations, nil
}

func (db *DB) checkApprovedVersionReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, a.approved_version_id, COALESCE(av.artifact_id, '')
FROM artifacts a
LEFT JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND a.approved_version_id IS NOT NULL
  AND (av.id IS NULL OR av.artifact_id != a.id)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, versionArtifactID string
		if err := rows.Scan(&artifactID, &versionID, &versionArtifactID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "approved_version_reference_invalid",
			Message: fmt.Sprintf("approved_version_id %s belongs to %s", versionID, versionArtifactID),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkLatestVersionReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, a.latest_version_id, COALESCE(av.artifact_id, '')
FROM artifacts a
LEFT JOIN artifact_versions av ON av.id = a.latest_version_id
WHERE a.project_id = ?
  AND a.latest_version_id IS NOT NULL
  AND (av.id IS NULL OR av.artifact_id != a.id)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, versionArtifactID string
		if err := rows.Scan(&artifactID, &versionID, &versionArtifactID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "latest_version_reference_invalid",
			Message: fmt.Sprintf("latest_version_id %s belongs to %s", versionID, versionArtifactID),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkApprovedVersionStatuses(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, av.id, av.status
FROM artifacts a
JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND av.status NOT IN ('approved', 'approved_with_notes')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, status string
		if err := rows.Scan(&artifactID, &versionID, &status); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "approved_version_status_invalid",
			Message: fmt.Sprintf("approved_version_id %s has status %s", versionID, status),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkApprovedWithNotes(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT av.id
FROM artifact_versions av
JOIN artifacts a ON a.id = av.artifact_id
WHERE a.project_id = ?
  AND av.status = 'approved_with_notes'
  AND TRIM(COALESCE(av.approval_notes, '')) = ''`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var versionID string
		if err := rows.Scan(&versionID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact_version",
			ID:      versionID,
			Code:    "approved_with_notes_missing_notes",
			Message: "approved_with_notes artifact version requires approval_notes",
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkReadyTaskArtifactContext(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT t.id
FROM tasks t
WHERE t.project_id = ?
  AND t.status = 'ready'
  AND NOT (
    EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'prd' AND av.status IN ('approved', 'approved_with_notes'))
    AND EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'architecture' AND av.status IN ('approved', 'approved_with_notes'))
    AND EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'roadmap' AND av.status IN ('approved', 'approved_with_notes'))
  )`, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "task",
			ID:      taskID,
			Code:    "ready_task_missing_trusted_artifacts",
			Message: "ready task requires approved PRD, architecture, and roadmap artifacts",
		})
	}
	return violations, rows.Err()
}
