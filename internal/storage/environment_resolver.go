package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func (db *DB) ResolveCanonicalGitEnvironment(ctx context.Context, projectID string) (platform.ExecutionEnvironment, error) {
	var envID sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
SELECT git_environment_id
FROM project_run_profiles
WHERE project_id = ? AND status = 'active'
ORDER BY updated_at DESC
LIMIT 1`, projectID).Scan(&envID); err != nil && err != sql.ErrNoRows {
		return platform.ExecutionEnvironment{}, err
	}
	if envID.Valid && strings.TrimSpace(envID.String) != "" {
		return db.environmentByID(ctx, projectID, envID.String)
	}
	return db.primaryEnvironment(ctx, projectID)
}

func (db *DB) ResolveImplementationEnvironment(ctx context.Context, projectID string) (platform.ExecutionEnvironment, error) {
	var envID sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
SELECT implementation_environment_id
FROM project_run_profiles
WHERE project_id = ? AND status = 'active'
ORDER BY updated_at DESC
LIMIT 1`, projectID).Scan(&envID); err != nil && err != sql.ErrNoRows {
		return platform.ExecutionEnvironment{}, err
	}
	if envID.Valid && strings.TrimSpace(envID.String) != "" {
		return db.environmentByID(ctx, projectID, envID.String)
	}
	return db.primaryEnvironment(ctx, projectID)
}

func (db *DB) environmentByID(ctx context.Context, projectID string, envID string) (platform.ExecutionEnvironment, error) {
	var env platform.ExecutionEnvironment
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, os_family, role, shell, project_root, git_provider, codex_adapter, sandbox_profile, status
FROM execution_environments
WHERE project_id = ? AND id = ?
LIMIT 1`, projectID, envID).Scan(&env.ID, &env.OSFamily, &env.Role, &env.Shell, &env.ProjectRoot, &env.GitProvider, &env.CodexAdapter, &env.SandboxProfile, &env.Status); err != nil {
		if err == sql.ErrNoRows {
			return platform.ExecutionEnvironment{}, fmt.Errorf("execution environment not found: %s", envID)
		}
		return platform.ExecutionEnvironment{}, err
	}
	if env.GitProvider == platform.GitProviderNone {
		return platform.ExecutionEnvironment{}, fmt.Errorf("canonical git environment %s has no git provider", env.ID)
	}
	if strings.TrimSpace(env.ProjectRoot) == "" {
		return platform.ExecutionEnvironment{}, fmt.Errorf("canonical git environment %s has empty project_root", env.ID)
	}
	return env, nil
}
