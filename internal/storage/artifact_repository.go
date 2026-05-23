package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ArtifactType string

const (
	ArtifactPRD          ArtifactType = "prd"
	ArtifactArchitecture ArtifactType = "architecture"
	ArtifactRoadmap      ArtifactType = "roadmap"
	ArtifactTaskYAML     ArtifactType = "task_yaml"
)

type ArtifactVersionInput struct {
	ProjectID    string
	ArtifactType ArtifactType
	Path         string
	Content      []byte
	Status       string
}

type ArtifactVersionRecord struct {
	ArtifactID string `json:"artifact_id"`
	VersionID  string `json:"version_id"`
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Path       string `json:"path"`
	Hash       string `json:"content_hash"`
}

func (db *DB) SaveArtifactVersion(ctx context.Context, input ArtifactVersionInput) (ArtifactVersionRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ArtifactVersionRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(string(input.ArtifactType)) == "" {
		return ArtifactVersionRecord{}, fmt.Errorf("artifact type is required")
	}
	if strings.TrimSpace(input.Path) == "" {
		return ArtifactVersionRecord{}, fmt.Errorf("artifact path is required")
	}
	if input.Status == "" {
		input.Status = "proposed"
	}
	contentHash := sha256Hex(input.Content)
	artifactID := artifactID(input.ProjectID, input.ArtifactType)

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ArtifactVersionRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := ensureArtifact(ctx, tx, input.ProjectID, artifactID, input.ArtifactType, now); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if existing, ok, err := latestArtifactVersionByHash(ctx, tx, artifactID, contentHash); err != nil {
		return ArtifactVersionRecord{}, err
	} else if ok {
		committed = true
		if err := tx.Commit(); err != nil {
			return ArtifactVersionRecord{}, err
		}
		return existing, nil
	}

	version, err := nextArtifactVersion(ctx, tx, artifactID)
	if err != nil {
		return ArtifactVersionRecord{}, err
	}
	versionID := artifactVersionID(artifactID, version)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO artifact_versions(
  id, artifact_id, version, status, path, content_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		versionID, artifactID, version, input.Status, input.Path, contentHash, now,
	); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE artifacts
SET latest_version_id = ?, status = ?, updated_at = ?
WHERE id = ?`, versionID, input.Status, now, artifactID); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "artifact_version_created", map[string]any{
		"artifact_id": artifactID,
		"version_id":  versionID,
		"type":        input.ArtifactType,
		"status":      input.Status,
	}, now); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ArtifactVersionRecord{}, err
	}
	committed = true
	return ArtifactVersionRecord{
		ArtifactID: artifactID,
		VersionID:  versionID,
		Version:    version,
		Status:     input.Status,
		Path:       input.Path,
		Hash:       contentHash,
	}, nil
}

func (db *DB) ApproveArtifactVersion(ctx context.Context, projectID string, artifactID string, version int, status string, notes string) (ArtifactVersionRecord, error) {
	if status != "approved" && status != "approved_with_notes" && status != "rejected" {
		return ArtifactVersionRecord{}, fmt.Errorf("unsupported artifact approval status: %s", status)
	}
	if status == "approved_with_notes" && strings.TrimSpace(notes) == "" {
		return ArtifactVersionRecord{}, fmt.Errorf("approved_with_notes requires notes")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ArtifactVersionRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	record, err := artifactVersionByNumber(ctx, tx, artifactID, version)
	if err != nil {
		return ArtifactVersionRecord{}, err
	}
	if !artifactBelongsToProject(ctx, tx, projectID, artifactID) {
		return ArtifactVersionRecord{}, fmt.Errorf("artifact %s does not belong to project %s", artifactID, projectID)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE artifact_versions
SET status = ?, reviewed_by = 'human', reviewed_at = ?, approval_notes = ?, rejected_reason = ?
WHERE id = ?`,
		status, now, nullableNotes(status, notes), nullableRejectedReason(status, notes), record.VersionID,
	); err != nil {
		return ArtifactVersionRecord{}, err
	}
	approvedVersionID := any(nil)
	if status == "approved" || status == "approved_with_notes" {
		approvedVersionID = record.VersionID
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE artifacts
SET status = ?, approved_version_id = COALESCE(?, approved_version_id), updated_at = ?
WHERE id = ?`, status, approvedVersionID, now, artifactID); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "artifact_version_reviewed", map[string]any{
		"artifact_id": artifactID,
		"version_id":  record.VersionID,
		"status":      status,
	}, now); err != nil {
		return ArtifactVersionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ArtifactVersionRecord{}, err
	}
	committed = true
	record.Status = status
	return record, nil
}

func ensureArtifact(ctx context.Context, tx *sql.Tx, projectID string, artifactID string, artifactType ArtifactType, now string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO artifacts(
  id, project_id, artifact_type, status, created_at, updated_at
) VALUES (?, ?, ?, 'draft', ?, ?)
ON CONFLICT(id) DO NOTHING`, artifactID, projectID, artifactType, now, now)
	return err
}

func latestArtifactVersionByHash(ctx context.Context, tx *sql.Tx, artifactID string, contentHash string) (ArtifactVersionRecord, bool, error) {
	var record ArtifactVersionRecord
	err := tx.QueryRowContext(ctx, `
SELECT artifact_id, id, version, status, path, content_hash
FROM artifact_versions
WHERE artifact_id = ? AND content_hash = ?
ORDER BY version DESC
LIMIT 1`, artifactID, contentHash).Scan(&record.ArtifactID, &record.VersionID, &record.Version, &record.Status, &record.Path, &record.Hash)
	if err == sql.ErrNoRows {
		return ArtifactVersionRecord{}, false, nil
	}
	if err != nil {
		return ArtifactVersionRecord{}, false, err
	}
	return record, true, nil
}

func nextArtifactVersion(ctx context.Context, tx *sql.Tx, artifactID string) (int, error) {
	var next sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(version) + 1 FROM artifact_versions WHERE artifact_id = ?", artifactID).Scan(&next); err != nil {
		return 0, err
	}
	if !next.Valid {
		return 1, nil
	}
	return int(next.Int64), nil
}

func artifactVersionByNumber(ctx context.Context, tx *sql.Tx, artifactID string, version int) (ArtifactVersionRecord, error) {
	var record ArtifactVersionRecord
	if err := tx.QueryRowContext(ctx, `
SELECT artifact_id, id, version, status, path, content_hash
FROM artifact_versions
WHERE artifact_id = ? AND version = ?`, artifactID, version).Scan(&record.ArtifactID, &record.VersionID, &record.Version, &record.Status, &record.Path, &record.Hash); err != nil {
		if err == sql.ErrNoRows {
			return ArtifactVersionRecord{}, fmt.Errorf("artifact version not found: %s v%d", artifactID, version)
		}
		return ArtifactVersionRecord{}, err
	}
	return record, nil
}

func artifactBelongsToProject(ctx context.Context, tx *sql.Tx, projectID string, artifactID string) bool {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM artifacts WHERE id = ? AND project_id = ?", artifactID, projectID).Scan(&count); err != nil {
		return false
	}
	return count == 1
}

func nullableNotes(status string, notes string) any {
	if status == "approved_with_notes" {
		return notes
	}
	return nil
}

func nullableRejectedReason(status string, notes string) any {
	if status == "rejected" {
		return notes
	}
	return nil
}

func artifactID(projectID string, artifactType ArtifactType) string {
	return "ART-" + stableShortHash(projectID+"|"+string(artifactType))
}

func artifactVersionID(artifactID string, version int) string {
	return fmt.Sprintf("%s-V%03d", artifactID, version)
}
