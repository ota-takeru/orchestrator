package storage

import (
	"embed"
	"fmt"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func RegisteredMigrations() ([]Migration, error) {
	definitions := []struct {
		version int
		name    string
		file    string
	}{
		{1, "platform_core", "migrations/001_platform_core.sql"},
		{2, "merge", "migrations/002_merge.sql"},
		{3, "worktree_safety_runs", "migrations/003_worktree_safety_runs.sql"},
		{4, "task_verification_commands", "migrations/004_task_verification_commands.sql"},
		{5, "publish_run_type", "migrations/005_publish_run_type.sql"},
		{6, "request_queue", "migrations/006_request_queue.sql"},
		{7, "planning_runs", "migrations/007_planning_runs.sql"},
		{8, "planning_consolidation", "migrations/008_planning_consolidation.sql"},
		{9, "worker_runs", "migrations/009_worker_runs.sql"},
		{10, "change_request_analysis", "migrations/010_change_request_analysis.sql"},
		{11, "environment_bindings", "migrations/011_environment_bindings.sql"},
		{12, "semantic_behavior_diff_details", "migrations/012_semantic_behavior_diff_details.sql"},
		{13, "memories", "migrations/013_memories.sql"},
	}
	migrations := make([]Migration, 0, len(definitions))
	for _, definition := range definitions {
		sqlBytes, err := migrationFiles.ReadFile(definition.file)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", definition.file, err)
		}
		migrations = append(migrations, NewMigration(definition.version, definition.name, string(sqlBytes)))
	}
	if err := ValidateMigrations(migrations); err != nil {
		return nil, err
	}
	return migrations, nil
}
