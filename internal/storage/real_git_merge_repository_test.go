package storage

import (
	"context"
	"encoding/json"
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
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        "RUN-001",
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content:      []byte("diff --git a/feature.txt b/feature.txt\n"),
	}); err != nil {
		t.Fatal(err)
	}
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
	if result.Status != "succeeded" || result.CandidateOID != candidate || result.ReverifyRunID == "" {
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
	var reverifyCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE id = ? AND run_type = 'reverify'", result.ReverifyRunID).Scan(&reverifyCount); err != nil {
		t.Fatal(err)
	}
	if reverifyCount != 1 {
		t.Fatalf("reverify count = %d", reverifyCount)
	}
}

func TestProcessRealGitMergeRequiresExecute(t *testing.T) {
	db := openMigratedTestDB(t)
	if _, err := db.ProcessRealGitMerge(context.Background(), "PROJECT-001", RealGitMergeInput{FFOnly: true, NoPush: true}); err == nil {
		t.Fatal("expected --execute to be required")
	}
}

func TestProcessRealGitMergeBlocksWithoutSavedDiffArtifact(t *testing.T) {
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
	if result.Status != "blocked" || result.FailureClass != "missing_diff_artifact" {
		t.Fatalf("result = %#v", result)
	}
	if got := gitOutput(t, repo, "rev-parse", "refs/heads/main"); got != base {
		t.Fatalf("main changed to %s, want %s", got, base)
	}
}

func TestSaveRealGitMergeEvidenceRecordsFailureClassAndRollback(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectID := "PROJECT-001"
	insertProject(t, db.SQL(), projectID)
	insertTask(t, db, projectID, "TASK-001", "queued_for_merge")
	entry := MergeQueueEntry{ID: "MERGE-001", TaskID: "TASK-001", Status: "queued", BaseCommit: "BASE", HeadCommit: "HEAD"}

	if err := db.saveRealGitMergeEvidence(ctx, projectID, entry, "RUN-ROLLBACK", 1, "main", "BASE", "HEAD", "failed", []string{"database merge state update failed after target ref update"}, "db_state_sync_failed", "failed", "git update-ref failed"); err != nil {
		t.Fatal(err)
	}

	var relPath string
	if err := db.SQL().QueryRowContext(ctx, `
SELECT path FROM run_artifacts
WHERE run_id = 'RUN-ROLLBACK' AND artifact_key = 'real-git-merge-summary.json'`).Scan(&relPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(db.dataRoot, relPath))
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["failure_class"] != "db_state_sync_failed" || summary["rollback_status"] != "failed" || summary["rollback_error"] != "git update-ref failed" {
		t.Fatalf("summary = %#v", summary)
	}
	var evidenceJSON string
	if err := db.SQL().QueryRowContext(ctx, `
SELECT evidence_json FROM workflow_events
WHERE project_id = ? AND event_type = 'real_git_merge_failed'`, projectID).Scan(&evidenceJSON); err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["failure_class"] != "db_state_sync_failed" || evidence["rollback_status"] != "failed" {
		t.Fatalf("evidence = %#v", evidence)
	}
}
