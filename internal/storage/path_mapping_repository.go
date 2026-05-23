package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/pathmap"
	"github.com/ota-takeru/orchestrator/internal/platform"
)

type PathMappingInput struct {
	ProjectID               string
	FromEnvironmentID       string
	ToEnvironmentID         string
	FromRoot                string
	ToRoot                  string
	Mode                    platform.MappingMode
	WriteOwnerEnvironmentID string
}

type PathMappingRecord struct {
	ID                      string               `json:"id"`
	FromEnvironmentID       string               `json:"from_environment_id"`
	ToEnvironmentID         string               `json:"to_environment_id"`
	FromRoot                string               `json:"from_root"`
	ToRoot                  string               `json:"to_root"`
	Mode                    platform.MappingMode `json:"mapping_mode"`
	WriteOwnerEnvironmentID string               `json:"write_owner_environment_id,omitempty"`
	Status                  string               `json:"status"`
}

func (db *DB) BuildPathMappingService(ctx context.Context, projectID string) (*pathmap.Service, error) {
	envRows, err := db.sql.QueryContext(ctx, `
SELECT id, os_family, project_root
FROM execution_environments
WHERE project_id = ? AND status IN ('detected', 'configured', 'ready')`, projectID)
	if err != nil {
		return nil, err
	}
	defer envRows.Close()
	var envs []pathmap.Environment
	for envRows.Next() {
		var env pathmap.Environment
		if err := envRows.Scan(&env.ID, &env.OSFamily, &env.AllowedRoot); err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	if err := envRows.Err(); err != nil {
		return nil, err
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("execution environments not found for project %s", projectID)
	}

	mappingRows, err := db.sql.QueryContext(ctx, `
SELECT from_environment_id, to_environment_id, from_root, to_root,
       mapping_mode, COALESCE(write_owner_environment_id, '')
FROM path_mappings
WHERE project_id = ? AND status = 'active'`, projectID)
	if err != nil {
		return nil, err
	}
	defer mappingRows.Close()
	var mappings []pathmap.Mapping
	for mappingRows.Next() {
		var mapping pathmap.Mapping
		if err := mappingRows.Scan(
			&mapping.FromEnvironmentID,
			&mapping.ToEnvironmentID,
			&mapping.FromRoot,
			&mapping.ToRoot,
			&mapping.Mode,
			&mapping.WriteOwnerEnvironmentID,
		); err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	if err := mappingRows.Err(); err != nil {
		return nil, err
	}
	return pathmap.NewService(envs, mappings)
}

func (db *DB) SavePathMapping(ctx context.Context, input PathMappingInput) (PathMappingRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return PathMappingRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.FromEnvironmentID) == "" || strings.TrimSpace(input.ToEnvironmentID) == "" {
		return PathMappingRecord{}, fmt.Errorf("from and to environment ids are required")
	}
	if strings.TrimSpace(input.FromRoot) == "" || strings.TrimSpace(input.ToRoot) == "" {
		return PathMappingRecord{}, fmt.Errorf("from and to roots are required")
	}
	if !platform.ValidMappingMode(input.Mode) {
		return PathMappingRecord{}, fmt.Errorf("invalid mapping mode: %s", input.Mode)
	}
	if input.Mode == platform.MappingSameFilesystem && strings.TrimSpace(input.WriteOwnerEnvironmentID) == "" {
		return PathMappingRecord{}, fmt.Errorf("same_filesystem mapping requires write owner")
	}

	fromEnv, err := db.pathEnvironment(ctx, input.ProjectID, input.FromEnvironmentID)
	if err != nil {
		return PathMappingRecord{}, err
	}
	toEnv, err := db.pathEnvironment(ctx, input.ProjectID, input.ToEnvironmentID)
	if err != nil {
		return PathMappingRecord{}, err
	}
	if input.WriteOwnerEnvironmentID != "" {
		if _, err := db.pathEnvironment(ctx, input.ProjectID, input.WriteOwnerEnvironmentID); err != nil {
			return PathMappingRecord{}, err
		}
	}
	if err := pathmap.ValidatePath(fromEnv, input.FromRoot, pathmap.PurposeRead); err != nil {
		return PathMappingRecord{}, fmt.Errorf("invalid from root: %w", err)
	}
	if err := pathmap.ValidatePath(toEnv, input.ToRoot, pathmap.PurposeRead); err != nil {
		return PathMappingRecord{}, fmt.Errorf("invalid to root: %w", err)
	}
	mapping := pathmap.Mapping{
		FromEnvironmentID:       input.FromEnvironmentID,
		ToEnvironmentID:         input.ToEnvironmentID,
		FromRoot:                input.FromRoot,
		ToRoot:                  input.ToRoot,
		Mode:                    input.Mode,
		WriteOwnerEnvironmentID: input.WriteOwnerEnvironmentID,
	}
	if _, err := pathmap.NewService([]pathmap.Environment{fromEnv, toEnv}, []pathmap.Mapping{mapping}); err != nil {
		return PathMappingRecord{}, err
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return PathMappingRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mappingID := "PATHMAP-" + stableShortHash(strings.Join([]string{
		input.ProjectID,
		input.FromEnvironmentID,
		input.ToEnvironmentID,
		input.FromRoot,
		input.ToRoot,
	}, "|"))
	if _, err := tx.ExecContext(ctx, `
INSERT INTO path_mappings(
  id, project_id, from_environment_id, to_environment_id, from_root, to_root,
  mapping_mode, write_owner_environment_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), 'active', ?, ?)
ON CONFLICT(project_id, from_environment_id, to_environment_id, from_root, to_root) DO UPDATE SET
  mapping_mode = excluded.mapping_mode,
  write_owner_environment_id = excluded.write_owner_environment_id,
  status = 'active',
  updated_at = excluded.updated_at`,
		mappingID, input.ProjectID, input.FromEnvironmentID, input.ToEnvironmentID,
		input.FromRoot, input.ToRoot, input.Mode, input.WriteOwnerEnvironmentID, now, now,
	); err != nil {
		return PathMappingRecord{}, err
	}
	if err := syncPathMappingIssueInbox(ctx, tx, input.ProjectID, mappingID, input, now); err != nil {
		return PathMappingRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "path_mapping_configured", map[string]any{
		"path_mapping_id":     mappingID,
		"from_environment_id": input.FromEnvironmentID,
		"to_environment_id":   input.ToEnvironmentID,
		"mapping_mode":        input.Mode,
	}, now); err != nil {
		return PathMappingRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return PathMappingRecord{}, err
	}
	committed = true
	return PathMappingRecord{
		ID:                      mappingID,
		FromEnvironmentID:       input.FromEnvironmentID,
		ToEnvironmentID:         input.ToEnvironmentID,
		FromRoot:                input.FromRoot,
		ToRoot:                  input.ToRoot,
		Mode:                    input.Mode,
		WriteOwnerEnvironmentID: input.WriteOwnerEnvironmentID,
		Status:                  "active",
	}, nil
}

func syncPathMappingIssueInbox(ctx context.Context, tx *sql.Tx, projectID string, mappingID string, input PathMappingInput, now string) error {
	dedupeKey := strings.Join([]string{projectID, "path_mapping_issue", mappingID}, ":")
	if input.Mode != platform.MappingUnsupported {
		_, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND dedupe_key = ? AND status = 'open'`,
			now, now, projectID, dedupeKey,
		)
		return err
	}
	itemID := "INBOX-" + stableShortHash(dedupeKey)
	title := "Path mapping unsupported: " + input.FromEnvironmentID + " -> " + input.ToEnvironmentID
	body := "Configure a supported path mapping mode before using this environment pair."
	_, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, 'path_mapping_issue', 'open', 'path_mapping', ?, ?, ?, 70, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = 'open',
  title = excluded.title,
  body = excluded.body,
  resolved_at = NULL,
  updated_at = excluded.updated_at`,
		itemID, projectID, mappingID, dedupeKey,
		projectID+":path_mapping_issue",
		title, body, now, now,
	)
	return err
}

func (db *DB) ValidateWorktreePath(ctx context.Context, projectID string, environmentID string, worktreePath string) error {
	service, err := db.BuildPathMappingService(ctx, projectID)
	if err != nil {
		return err
	}
	return service.ValidatePathInEnvironment(ctx, environmentID, worktreePath, pathmap.PurposeWorktree)
}

func (db *DB) pathEnvironment(ctx context.Context, projectID string, environmentID string) (pathmap.Environment, error) {
	var env pathmap.Environment
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, os_family, project_root
FROM execution_environments
WHERE project_id = ? AND id = ?`, projectID, environmentID).Scan(
		&env.ID,
		&env.OSFamily,
		&env.AllowedRoot,
	); err != nil {
		if err == sql.ErrNoRows {
			return pathmap.Environment{}, fmt.Errorf("execution environment not found: %s", environmentID)
		}
		return pathmap.Environment{}, err
	}
	return env, nil
}
