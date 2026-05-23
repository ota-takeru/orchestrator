package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestBuildPathMappingServiceFromStorage(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	insertEnvironmentWithRoot(t, db, "wsl-sidecar", "PROJECT-001", "sidecar", "/sidecar")
	insertPathMapping(t, db, "PROJECT-001", "linux-main", "wsl-sidecar", "/repo", "/sidecar", platform.MappingIsolatedWorktree, "")

	service, err := db.BuildPathMappingService(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := service.ToEnvironmentPath(ctx, "linux-main", "wsl-sidecar", "/repo/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if mapped != "/sidecar/src/main.go" {
		t.Fatalf("mapped path = %s", mapped)
	}
}

func TestValidateWorktreePathRejectsOutsideEnvironmentRoot(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	if err := db.ValidateWorktreePath(ctx, "PROJECT-001", "linux-main", "/repo/.devagent-worktrees/TASK-001"); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateWorktreePath(ctx, "PROJECT-001", "linux-main", "/tmp/TASK-001"); err == nil {
		t.Fatal("expected outside worktree path to be rejected")
	}
}

func TestBuildPathMappingServiceRejectsSameFilesystemWithoutOwner(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", "/repo")
	insertEnvironmentWithRoot(t, db, "wsl-sidecar", "PROJECT-001", "sidecar", "/mnt/repo")
	insertPathMapping(t, db, "PROJECT-001", "linux-main", "wsl-sidecar", "/repo", "/mnt/repo", platform.MappingSameFilesystem, "")
	if _, err := db.BuildPathMappingService(ctx, "PROJECT-001"); err == nil {
		t.Fatal("expected same_filesystem mapping without write owner to fail")
	}
}

func insertPathMapping(t *testing.T, db *DB, projectID string, fromEnvID string, toEnvID string, fromRoot string, toRoot string, mode platform.MappingMode, writeOwner string) {
	t.Helper()
	id := "PATHMAP-" + stableShortHash(projectID+"|"+fromEnvID+"|"+toEnvID+"|"+fromRoot+"|"+toRoot)
	var owner any
	if writeOwner != "" {
		owner = writeOwner
	}
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO path_mappings(
  id, project_id, from_environment_id, to_environment_id, from_root, to_root,
  mapping_mode, write_owner_environment_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		id, projectID, fromEnvID, toEnvID, fromRoot, toRoot, mode, owner, now(), now(),
	); err != nil {
		t.Fatal(err)
	}
}
