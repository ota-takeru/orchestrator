package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateAppliesRegisteredMigrations(t *testing.T) {
	db := openMigratedTestDB(t)
	applied, err := db.AppliedMigrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied migrations = %d, want 2", len(applied))
	}
}

func TestMigrationRejectsChecksumDrift(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	migrations[0].SQL += "\n-- changed"
	migrations[0] = NewMigration(migrations[0].Version, migrations[0].Name, migrations[0].SQL)
	if err := db.Migrate(ctx, migrations); err == nil {
		t.Fatal("expected checksum drift to fail")
	}
}

func TestSQLiteForeignKeysAreEnforced(t *testing.T) {
	db := openMigratedTestDB(t)
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (
  'ENV-001', 'PROJECT-MISSING', 'linux', 'primary', 'bash', '/repo',
  'linux-git', 'codex-linux', 'linux-bubblewrap', 'detected', ?, ?
)`, now(), now())
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
}

func TestSQLiteCheckValuesRejectInvalidTaskStatus(t *testing.T) {
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO tasks(
  id, project_id, status, title, base_branch, created_at, updated_at
) VALUES (
  'TASK-001', 'PROJECT-001', 'not_a_state', 'Task', 'main', ?, ?
)`, now(), now())
	if err == nil {
		t.Fatal("expected CHECK violation for task status")
	}
}

func TestSQLiteAllowsExactlyOnePrimaryEnvironmentPerProject(t *testing.T) {
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "ENV-001", "PROJECT-001", "primary")
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (
  'ENV-002', 'PROJECT-001', 'linux', 'primary', 'bash', '/repo2',
  'linux-git', 'codex-linux', 'linux-bubblewrap', 'detected', ?, ?
)`, now(), now())
	if err == nil {
		t.Fatal("expected unique primary environment violation")
	}
}

func openMigratedTestDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	return db
}

func insertProject(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
INSERT INTO projects(
  id, name, root_path, lifecycle_status, archive_status, created_at, updated_at
) VALUES (?, 'Project', '/repo', 'concept', 'active', ?, ?)`, id, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

func insertEnvironment(t *testing.T, db *sql.DB, id string, projectID string, role string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (
  ?, ?, 'linux', ?, 'bash', '/repo', 'linux-git',
  'codex-linux', 'linux-bubblewrap', 'detected', ?, ?
)`, id, projectID, role, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
