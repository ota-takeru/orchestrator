package storage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunWorktreeSafetyCheckRecordsMissingWorktreeBlocker(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := t.TempDir()
	insertProjectWithRoot(t, db, "PROJECT-001", repo)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", repo)
	insertTask(t, db, "PROJECT-001", "TASK-001", "merged")
	runID, err := db.createTerminalRun(ctx, "PROJECT-001", "TASK-001", "implementation", "succeeded", 1, "BASE", "HEAD", "DIFF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        runID,
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/fake.txt b/fake.txt\n"),
	}); err != nil {
		t.Fatal(err)
	}
	record, err := db.RunWorktreeSafetyCheck(ctx, "PROJECT-001", "TASK-001", "")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "failed" || len(record.Blockers) == 0 {
		t.Fatalf("record = %#v", record)
	}
	var runType, artifactKey string
	if err := db.SQL().QueryRowContext(ctx, "SELECT run_type FROM runs WHERE id = ?", record.RunID).Scan(&runType); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT artifact_key FROM run_artifacts WHERE run_id = ? AND artifact_type = 'summary'", record.RunID).Scan(&artifactKey); err != nil {
		t.Fatal(err)
	}
	if runType != "worktree_safety" || artifactKey != "worktree-safety-summary.json" {
		t.Fatalf("runType=%s artifactKey=%s", runType, artifactKey)
	}
}

func TestRunWorktreeSafetyCheckDetectsDirtyWorktree(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := t.TempDir()
	worktree := filepath.Join(repo, ".devagent-worktrees", "TASK-001")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRunInDir(t, worktree, "init")
	gitRunInDir(t, worktree, "config", "user.email", "test@example.com")
	gitRunInDir(t, worktree, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunInDir(t, worktree, "add", "tracked.txt")
	gitRunInDir(t, worktree, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(worktree, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	insertProjectWithRoot(t, db, "PROJECT-001", repo)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", repo)
	insertTask(t, db, "PROJECT-001", "TASK-001", "merged")
	runID, err := db.createTerminalRun(ctx, "PROJECT-001", "TASK-001", "implementation", "succeeded", 1, "BASE", "HEAD", "DIFF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        runID,
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/fake.txt b/fake.txt\n"),
	}); err != nil {
		t.Fatal(err)
	}

	record, err := db.RunWorktreeSafetyCheck(ctx, "PROJECT-001", "TASK-001", worktree)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "failed" {
		t.Fatalf("record = %#v", record)
	}
	var commandCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM command_events WHERE run_id = ? AND command_kind = 'git'", record.RunID).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if commandCount != 1 {
		t.Fatalf("command count = %d", commandCount)
	}
}

func gitRunInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
