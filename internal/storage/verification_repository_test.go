package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

func TestSaveVerificationReportPersistsMultipleEnvironmentResults(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "windows-main", "PROJECT-001", "primary")
	insertWSLEnvironment(t, db.SQL(), "wsl-sidecar", "PROJECT-001", "sidecar")

	commands := []verifier.Command{
		{
			ID:               "windows-test",
			EnvironmentID:    "windows-main",
			Runner:           "fake",
			WorkingDir:       `C:\repo`,
			Argv:             []string{"test"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		},
		{
			ID:               "wsl-test",
			EnvironmentID:    "wsl-sidecar",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"test"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: false,
		},
	}
	report, err := verifier.Run(ctx, "RUN-001", verifier.StaticRunnerRegistry{
		"windows-main": runners.NewFakeWindowsRunner("windows-main"),
		"wsl-sidecar":  runners.NewFakeWSLRunner("wsl-sidecar"),
	}, commands)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  "PROJECT-001",
		RunID:      "RUN-001",
		AttemptNo:  1,
		BaseCommit: "BASE",
		Commands:   commands,
		Report:     report,
	}); err != nil {
		t.Fatal(err)
	}

	var commandEvents, verificationResults, runArtifacts int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM command_events WHERE run_id = 'RUN-001'").Scan(&commandEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM verification_results WHERE run_id = 'RUN-001'").Scan(&verificationResults); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE run_id = 'RUN-001' AND artifact_type = 'command_stdout'").Scan(&runArtifacts); err != nil {
		t.Fatal(err)
	}
	if commandEvents != 2 || verificationResults != 2 || runArtifacts != 2 {
		t.Fatalf("command_events=%d verification_results=%d run_artifacts=%d", commandEvents, verificationResults, runArtifacts)
	}
}

func TestSaveVerificationReportMarksRequiredFailureRunFailed(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")

	commands := []verifier.Command{
		{
			ID:               "go-test",
			EnvironmentID:    "linux-main",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"fail"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		},
	}
	report, err := verifier.Run(ctx, "RUN-002", verifier.StaticRunnerRegistry{
		"linux-main": runners.NewFakeLinuxRunner("linux-main"),
	}, commands)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  "PROJECT-001",
		RunID:      "RUN-002",
		AttemptNo:  1,
		BaseCommit: "BASE",
		Commands:   commands,
		Report:     report,
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = 'RUN-002'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("run status = %s, want failed", status)
	}
}

func TestSaveVerificationReportRequiresReverifyContext(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:  "PROJECT-001",
		RunID:      "RUN-003",
		RunType:    "reverify",
		AttemptNo:  1,
		BaseCommit: "BASE",
	}); err == nil {
		t.Fatal("expected reverify context to be required")
	}
}

func insertWSLEnvironment(t *testing.T, db sqlExecer, id string, projectID string, role string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (
  ?, ?, ?, ?, 'bash', '/repo', 'linux-git',
  'codex-wsl', 'linux-bubblewrap', 'detected', ?, ?
)`, id, projectID, platform.OSFamilyWSL, role, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
