package storage

import (
	"context"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func (db *DB) ListExecutionEnvironments(ctx context.Context, projectID string) ([]platform.ExecutionEnvironment, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, os_family, role, shell, project_root, COALESCE(worktree_root, ''), git_provider, codex_adapter, sandbox_profile, status
FROM execution_environments
WHERE project_id = ?
ORDER BY role, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var envs []platform.ExecutionEnvironment
	for rows.Next() {
		var env platform.ExecutionEnvironment
		if err := rows.Scan(&env.ID, &env.OSFamily, &env.Role, &env.Shell, &env.ProjectRoot, &env.WorktreeRoot, &env.GitProvider, &env.CodexAdapter, &env.SandboxProfile, &env.Status); err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	return envs, rows.Err()
}
