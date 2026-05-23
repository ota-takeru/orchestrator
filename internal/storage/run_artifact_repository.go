package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RunArtifactInput struct {
	ProjectID       string
	RunID           string
	CommandEventID  *string
	ArtifactType    string
	ArtifactKey     string
	Content         []byte
	RedactionStatus string
}

type RunArtifactRecord struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

func (db *DB) SaveRunArtifact(ctx context.Context, input RunArtifactInput) (RunArtifactRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.RunID) == "" {
		return RunArtifactRecord{}, fmt.Errorf("project id and run id are required")
	}
	if strings.TrimSpace(input.ArtifactType) == "" || strings.TrimSpace(input.ArtifactKey) == "" {
		return RunArtifactRecord{}, fmt.Errorf("artifact type and key are required")
	}
	if input.RedactionStatus == "" {
		input.RedactionStatus = "not_needed"
	}
	artifactID := runArtifactID(input.RunID, input.ArtifactType, input.ArtifactKey)
	contentHash := sha256Hex(input.Content)
	relPath, err := db.writeRunArtifactFile(input.ProjectID, input.RunID, input.ArtifactKey, input.Content)
	if err != nil {
		return RunArtifactRecord{}, err
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return RunArtifactRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := insertRunArtifact(ctx, tx, input, artifactID, relPath, contentHash, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return RunArtifactRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunArtifactRecord{}, err
	}
	committed = true
	return RunArtifactRecord{ID: artifactID, Path: relPath, ContentHash: contentHash}, nil
}

func (db *DB) saveRunArtifactInTx(ctx context.Context, tx *sql.Tx, input RunArtifactInput, now string) (RunArtifactRecord, error) {
	if input.RedactionStatus == "" {
		input.RedactionStatus = "not_needed"
	}
	artifactID := runArtifactID(input.RunID, input.ArtifactType, input.ArtifactKey)
	contentHash := sha256Hex(input.Content)
	relPath, err := db.writeRunArtifactFile(input.ProjectID, input.RunID, input.ArtifactKey, input.Content)
	if err != nil {
		return RunArtifactRecord{}, err
	}
	if err := insertRunArtifact(ctx, tx, input, artifactID, relPath, contentHash, now); err != nil {
		return RunArtifactRecord{}, err
	}
	return RunArtifactRecord{ID: artifactID, Path: relPath, ContentHash: contentHash}, nil
}

func (db *DB) writeRunArtifactFile(projectID string, runID string, artifactKey string, content []byte) (string, error) {
	relPath := filepath.Join("projects", projectID, "runs", runID, artifactKey)
	absPath := filepath.Join(db.dataRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", err
	}
	tmpPath := absPath + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		return "", err
	}
	return relPath, nil
}

func insertRunArtifact(ctx context.Context, tx *sql.Tx, input RunArtifactInput, artifactID string, path string, contentHash string, now string) error {
	var commandEventID any
	if input.CommandEventID != nil {
		commandEventID = *input.CommandEventID
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO run_artifacts(
  id, project_id, run_id, command_event_id, artifact_type, artifact_key,
  path, content_hash, redaction_status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, artifact_type, artifact_key) DO UPDATE SET
  command_event_id = excluded.command_event_id,
  path = excluded.path,
  content_hash = excluded.content_hash,
  redaction_status = excluded.redaction_status`,
		artifactID, input.ProjectID, input.RunID, commandEventID, input.ArtifactType,
		input.ArtifactKey, path, contentHash, input.RedactionStatus, now,
	)
	return err
}

func runArtifactID(runID string, artifactType string, artifactKey string) string {
	return "RUNART-" + stableShortHash(runID+"|"+artifactType+"|"+artifactKey)
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
