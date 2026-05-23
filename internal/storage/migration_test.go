package storage

import (
	"strings"
	"testing"
)

func TestRegisteredMigrationsValidate(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 {
		t.Fatalf("migration count = %d, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Fatalf("unexpected migration versions: %#v", migrations)
	}
}

func TestPlanMigrationsRejectsChecksumDrift(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	_, err = PlanMigrations(migrations, []AppliedMigration{{Version: 1, Checksum: "changed"}})
	if err == nil {
		t.Fatal("expected checksum drift to fail")
	}
}

func TestMigration001ContainsPlatformCoreTables(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[0].SQL
	required := []string{
		"CREATE TABLE projects",
		"CREATE TABLE execution_environments",
		"CREATE TABLE project_run_profiles",
		"CREATE TABLE path_mappings",
		"CREATE TABLE toolchain_requirements",
		"CREATE TABLE command_events",
		"CREATE TABLE verification_results",
		"windows_primary",
		"required_for_merge",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 001 missing %q", token)
		}
	}
}

func TestMigration002ContainsMergeAndPatchTables(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[1].SQL
	for _, token := range []string{"CREATE TABLE merge_queue_entries", "CREATE TABLE patch_applications", "CREATE TABLE semantic_behavior_diffs"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 002 missing %q", token)
		}
	}
}
