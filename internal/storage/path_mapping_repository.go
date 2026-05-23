package storage

import (
	"context"
	"fmt"

	"github.com/ota-takeru/orchestrator/internal/pathmap"
)

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

func (db *DB) ValidateWorktreePath(ctx context.Context, projectID string, environmentID string, worktreePath string) error {
	service, err := db.BuildPathMappingService(ctx, projectID)
	if err != nil {
		return err
	}
	return service.ValidatePathInEnvironment(ctx, environmentID, worktreePath, pathmap.PurposeWorktree)
}
