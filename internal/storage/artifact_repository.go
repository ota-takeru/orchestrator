package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

type ArtifactRecord struct {
	ArtifactID        string       `json:"artifact_id"`
	ArtifactType      ArtifactType `json:"artifact_type"`
	Status            string       `json:"status"`
	LatestVersionID   string       `json:"latest_version_id,omitempty"`
	ApprovedVersionID string       `json:"approved_version_id,omitempty"`
	LatestVersion     int          `json:"latest_version,omitempty"`
	ApprovedVersion   int          `json:"approved_version,omitempty"`
	Path              string       `json:"path,omitempty"`
}

type TrustedArtifactContextRecord struct {
	ArtifactID    string       `json:"artifact_id"`
	ArtifactType  ArtifactType `json:"artifact_type"`
	VersionID     string       `json:"version_id"`
	Version       int          `json:"version"`
	Status        string       `json:"status"`
	Path          string       `json:"path"`
	ContentHash   string       `json:"content_hash"`
	ApprovalNotes string       `json:"approval_notes,omitempty"`
	ReviewedAt    string       `json:"reviewed_at,omitempty"`
}

type TrustedArtifactContentRecord struct {
	TrustedArtifactContextRecord
	Content string `json:"content"`
}

func (db *DB) ListArtifacts(ctx context.Context, projectID string, artifactType string) ([]ArtifactRecord, error) {
	query := `
SELECT a.id, a.artifact_type, a.status,
       COALESCE(a.latest_version_id, ''),
       COALESCE(a.approved_version_id, ''),
       COALESCE(latest.version, 0),
       COALESCE(approved.version, 0),
       COALESCE(latest.path, '')
FROM artifacts a
LEFT JOIN artifact_versions latest ON latest.id = a.latest_version_id
LEFT JOIN artifact_versions approved ON approved.id = a.approved_version_id
WHERE a.project_id = ?`
	args := []any{projectID}
	if strings.TrimSpace(artifactType) != "" {
		query += " AND a.artifact_type = ?"
		args = append(args, artifactType)
	}
	query += " ORDER BY a.artifact_type"

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []ArtifactRecord
	for rows.Next() {
		var artifact ArtifactRecord
		if err := rows.Scan(
			&artifact.ArtifactID,
			&artifact.ArtifactType,
			&artifact.Status,
			&artifact.LatestVersionID,
			&artifact.ApprovedVersionID,
			&artifact.LatestVersion,
			&artifact.ApprovedVersion,
			&artifact.Path,
		); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (db *DB) TrustedArtifactContext(ctx context.Context, projectID string) ([]TrustedArtifactContextRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, a.artifact_type, av.id, av.version, av.status, av.path, av.content_hash,
       COALESCE(av.approval_notes, ''), COALESCE(av.reviewed_at, '')
FROM artifacts a
JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND av.status IN ('approved', 'approved_with_notes')
ORDER BY a.artifact_type`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []TrustedArtifactContextRecord
	for rows.Next() {
		var record TrustedArtifactContextRecord
		if err := rows.Scan(
			&record.ArtifactID,
			&record.ArtifactType,
			&record.VersionID,
			&record.Version,
			&record.Status,
			&record.Path,
			&record.ContentHash,
			&record.ApprovalNotes,
			&record.ReviewedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
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
		if err := db.writeArtifactVersionSnapshot(input.ProjectID, existing.ArtifactID, existing.VersionID, existing.Path, input.Content); err != nil {
			return ArtifactVersionRecord{}, err
		}
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
	if err := db.writeArtifactVersionSnapshot(input.ProjectID, artifactID, versionID, input.Path, input.Content); err != nil {
		return ArtifactVersionRecord{}, err
	}
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
	previousApprovedVersionID, err := approvedArtifactVersionID(ctx, tx, artifactID)
	if err != nil {
		return ArtifactVersionRecord{}, err
	}
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
		if previousApprovedVersionID != "" && previousApprovedVersionID != record.VersionID {
			if _, err := tx.ExecContext(ctx, `
UPDATE artifact_versions
SET status = 'superseded'
WHERE id = ? AND status IN ('approved', 'approved_with_notes')`, previousApprovedVersionID); err != nil {
				return ArtifactVersionRecord{}, err
			}
		}
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

func approvedArtifactVersionID(ctx context.Context, tx *sql.Tx, artifactID string) (string, error) {
	var versionID sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT approved_version_id FROM artifacts WHERE id = ?", artifactID).Scan(&versionID); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("artifact not found: %s", artifactID)
		}
		return "", err
	}
	if !versionID.Valid {
		return "", nil
	}
	return versionID.String, nil
}

func (db *DB) TrustedArtifactContentBundle(ctx context.Context, projectID string) ([]TrustedArtifactContentRecord, error) {
	records, err := db.TrustedArtifactContext(ctx, projectID)
	if err != nil {
		return nil, err
	}
	bundle := make([]TrustedArtifactContentRecord, 0, len(records))
	for _, record := range records {
		content, err := db.readArtifactVersionSnapshot(projectID, record)
		if err != nil {
			return nil, err
		}
		bundle = append(bundle, TrustedArtifactContentRecord{
			TrustedArtifactContextRecord: record,
			Content:                      string(content),
		})
	}
	return bundle, nil
}

func (db *DB) writeArtifactVersionSnapshot(projectID string, artifactID string, versionID string, artifactPath string, content []byte) error {
	relPath := artifactVersionSnapshotPath(projectID, artifactID, versionID, artifactPath)
	absPath := filepath.Join(db.dataRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	tmpPath := absPath + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, absPath)
}

func (db *DB) readArtifactVersionSnapshot(projectID string, record TrustedArtifactContextRecord) ([]byte, error) {
	relPath := artifactVersionSnapshotPath(projectID, record.ArtifactID, record.VersionID, record.Path)
	content, err := os.ReadFile(filepath.Join(db.dataRoot, relPath))
	if err != nil {
		return nil, fmt.Errorf("read artifact version snapshot %s: %w", record.VersionID, err)
	}
	if hash := sha256Hex(content); hash != record.ContentHash {
		return nil, fmt.Errorf("artifact version snapshot hash mismatch: %s", record.VersionID)
	}
	return content, nil
}

func artifactVersionSnapshotPath(projectID string, artifactID string, versionID string, artifactPath string) string {
	name := filepath.Base(filepath.Clean(artifactPath))
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		name = "artifact"
	}
	return filepath.Join("projects", projectID, "artifacts", artifactID, versionID, name)
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
