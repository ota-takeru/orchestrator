package storage

import (
	"strings"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/statemachine"
)

func TestRegisteredMigrationsValidate(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 12 {
		t.Fatalf("migration count = %d, want 12", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 || migrations[2].Version != 3 || migrations[3].Version != 4 || migrations[4].Version != 5 || migrations[5].Version != 6 || migrations[6].Version != 7 || migrations[7].Version != 8 || migrations[8].Version != 9 || migrations[9].Version != 10 || migrations[10].Version != 11 || migrations[11].Version != 12 {
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

func TestMigration012AddsSemanticBehaviorDiffDetails(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[11].SQL
	for _, token := range []string{"ADD COLUMN category", "ADD COLUMN summary", "ADD COLUMN confidence", "idx_semantic_behavior_diffs_category"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 012 missing %q", token)
		}
	}
}

func TestMigration003AddsWorktreeSafetyRunTypes(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[2].SQL
	for _, token := range []string{"'cleanup'", "'worktree_safety'", "ALTER TABLE runs RENAME TO runs_old"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 003 missing %q", token)
		}
	}
}

func TestMigration004AddsTaskVerificationCommands(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[3].SQL
	for _, token := range []string{"ALTER TABLE tasks", "verification_commands_json", "DEFAULT '[]'"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 004 missing %q", token)
		}
	}
}

func TestMigration005AddsPublishRunType(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[4].SQL
	for _, token := range []string{"'publish'", "ALTER TABLE runs RENAME TO runs_old"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 005 missing %q", token)
		}
	}
}

func TestMigration006AddsRequestQueue(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[5].SQL
	for _, token := range []string{"CREATE TABLE work_queue_items", "feature_request_analysis", "idx_open_work_queue_idempotency"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 006 missing %q", token)
		}
	}
}

func TestMigration007AddsPlanningRuns(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[6].SQL
	for _, token := range []string{"CREATE TABLE planning_runs", "CREATE TABLE planning_artifacts", "CREATE TABLE decision_report_drafts", "idx_planning_runs_input"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 007 missing %q", token)
		}
	}
}

func TestMigration008AddsPlanningConsolidation(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[7].SQL
	for _, token := range []string{"ALTER TABLE task_groups ADD COLUMN planning_unit", "idx_task_groups_feature_request_open"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 008 missing %q", token)
		}
	}
}

func TestMigration009AddsWorkerRuns(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[8].SQL
	for _, token := range []string{"CREATE TABLE worker_runs", "idx_worker_runs_one_running_lane", "bounded_parallel"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 009 missing %q", token)
		}
	}
}

func TestMigration010AddsChangeRequestAnalysis(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[9].SQL
	for _, token := range []string{"ALTER TABLE planning_runs ADD COLUMN change_request_id", "idx_planning_artifacts_change_type_status"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 010 missing %q", token)
		}
	}
}

func TestMigration011AddsEnvironmentBindings(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := migrations[10].SQL
	for _, token := range []string{"CREATE TABLE environment_bindings", "CREATE TABLE environment_audit_events", "idx_env_binding_global_key_scope"} {
		if !strings.Contains(sql, token) {
			t.Fatalf("migration 011 missing %q", token)
		}
	}
}

func TestStorageCheckValuesCoverAllStateMachines(t *testing.T) {
	migrations, err := RegisteredMigrations()
	if err != nil {
		t.Fatal(err)
	}
	allSQL := migrations[0].SQL + "\n" + migrations[1].SQL + "\n" + migrations[2].SQL + "\n" + migrations[3].SQL + "\n" + migrations[4].SQL + "\n" + migrations[5].SQL + "\n" + migrations[6].SQL + "\n" + migrations[7].SQL + "\n" + migrations[8].SQL + "\n" + migrations[9].SQL + "\n" + migrations[10].SQL
	machines := []statemachine.Machine{
		statemachine.Task,
		statemachine.Run,
		statemachine.CommandEvent,
		statemachine.ExecutionEnvironment,
		statemachine.RunProfile,
		statemachine.PathMapping,
		statemachine.TargetPlatform,
		statemachine.ToolchainRequirement,
		statemachine.ProjectLifecycle,
		statemachine.ArtifactVersion,
		statemachine.Artifact,
		statemachine.HumanApproval,
		statemachine.MergeQueue,
		statemachine.PatchApplication,
	}
	for _, machine := range machines {
		for _, state := range machine.States() {
			if !strings.Contains(allSQL, "'"+state+"'") {
				t.Fatalf("state %q from %s is missing from storage CHECK values", state, machine.Name())
			}
		}
	}
}
