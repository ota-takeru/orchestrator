package storage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func TestRunMergeGitDryRunStoresGitEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	head := gitOutput(t, repo, "rev-parse", "HEAD")
	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)
	insertTask(t, db, projectID, "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, projectID, "TASK-001", "RUN-001", head, head)
	updateRunBaseCommit(t, db, "RUN-001", head)
	insertVerificationEvidence(t, db, projectID, "RUN-001", "linux-main", "VERIF-001")
	gates := []decisions.GateResult{{
		Status:   decisions.GatePass,
		Severity: decisions.SeverityLow,
		Detector: "verification_passed",
		Evidence: map[string]any{"run_id": "RUN-001"},
	}}
	if err := db.SaveGateResults(ctx, projectID, ptr("TASK-001"), "RUN-001", gates); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: projectID, TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: projectID, TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	entry, err := db.QueueTaskForMerge(ctx, projectID, "TASK-001")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.RunMergeGitDryRun(ctx, projectID, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Classification != "clean" || len(result.Blockers) != 0 {
		t.Fatalf("git dry-run result = %#v", result)
	}
	var commandCount, summaryCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM command_events WHERE run_id = ? AND command_kind = 'git'", result.RunID).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE run_id = ? AND artifact_key = 'git-dry-run-summary.json'", result.RunID).Scan(&summaryCount); err != nil {
		t.Fatal(err)
	}
	if commandCount != 5 || summaryCount != 1 {
		t.Fatalf("commandCount=%d summaryCount=%d", commandCount, summaryCount)
	}
}

func TestRunMergeGitDryRunReportsDirtyWorktree(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	head := gitOutput(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)
	insertTask(t, db, projectID, "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, projectID, "TASK-001", "RUN-001", head, head)
	updateRunBaseCommit(t, db, "RUN-001", head)
	insertVerificationEvidence(t, db, projectID, "RUN-001", "linux-main", "VERIF-001")
	gates := []decisions.GateResult{{
		Status:   decisions.GatePass,
		Severity: decisions.SeverityLow,
		Detector: "verification_passed",
		Evidence: map[string]any{"run_id": "RUN-001"},
	}}
	if err := db.SaveGateResults(ctx, projectID, ptr("TASK-001"), "RUN-001", gates); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: projectID, TaskID: "TASK-001", ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: projectID, TaskID: "TASK-001", ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	entry, err := db.QueueTaskForMerge(ctx, projectID, "TASK-001")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.RunMergeGitDryRun(ctx, projectID, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Classification != "dirty_worktree" || len(result.Blockers) == 0 {
		t.Fatalf("dirty git dry-run result = %#v", result)
	}
}

func initStorageGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	return repo
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func updateRunBaseCommit(t *testing.T, db *DB, runID string, baseCommit string) {
	t.Helper()
	if _, err := db.SQL().ExecContext(context.Background(), "UPDATE runs SET base_commit = ? WHERE id = ?", baseCommit, runID); err != nil {
		t.Fatal(err)
	}
}

func insertEnvironmentWithRoot(t *testing.T, db *DB, id string, projectID string, role string, root string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (
  ?, ?, 'linux', ?, 'bash', ?, 'linux-git',
  'codex-linux', 'linux-bubblewrap', 'detected', ?, ?
)`, id, projectID, role, root, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}
