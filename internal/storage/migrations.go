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
