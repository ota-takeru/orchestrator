package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func TestProcessRealGitMergeFastForwardsLocalMain(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	base := gitOutput(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", "-b", "candidate")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "feature.txt")
	gitRun(t, repo, "commit", "-m", "feature")
	candidate := gitOutput(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", "main")

	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)
	insertTask(t, db, projectID, "TASK-001", "ready_for_human_review")
	insertSucceededRun(t, db, projectID, "TASK-001", "RUN-001", candidate, "DIFF")
	updateRunBaseCommit(t, db, "RUN-001", base)
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

	result, err := db.ProcessRealGitMerge(ctx, projectID, RealGitMergeInput{
		EntryID: entry.ID,
		Target:  "main",
		Execute: true,
		FFOnly:  true,
		NoPush:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.CandidateOID != candidate {
		t.Fatalf("result = %#v", result)
	}
	if got := gitOutput(t, repo, "rev-parse", "refs/heads/main"); got != candidate {
		t.Fatalf("main = %s, want %s", got, candidate)
	}
	var taskStatus, queueStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM merge_queue_entries WHERE id = ?", entry.ID).Scan(&queueStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "merged" || queueStatus != "merged" {
		t.Fatalf("task=%s queue=%s", taskStatus, queueStatus)
	}
}

func TestProcessRealGitMergeRequiresExecute(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.ProcessRealGitMerge(context.Background(), "PROJECT-001", RealGitMergeInput{FFOnly: true, NoPush: true}); err == nil {
		t.Fatal("expected --execute to be required")
	}
}
