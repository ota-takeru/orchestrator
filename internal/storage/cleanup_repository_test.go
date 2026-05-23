package storage

import (
	"context"
	"testing"
)

func TestBuildCleanupDryRunPlanListsTerminalTasks(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
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

	plan, err := db.BuildCleanupDryRunPlan(ctx, "PROJECT-001", CleanupPlanOptions{IncludeMerged: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].TaskID != "TASK-001" || plan[0].Status != "merged" {
		t.Fatalf("cleanup plan = %#v", plan)
	}
	if plan[0].Eligible {
		t.Fatal("dry-run cleanup should not mark deletion eligible before worktree deletion is implemented")
	}
}

func TestBuildCleanupDryRunPlanSkipsNonTerminalTasks(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	plan, err := db.BuildCleanupDryRunPlan(ctx, "PROJECT-001", CleanupPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 0 {
		t.Fatalf("cleanup plan = %#v", plan)
	}
}
