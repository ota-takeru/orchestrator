package storage

import (
	"context"
	"testing"
)

func TestReviewTaskCreatesSemanticBehaviorDiff(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready_for_human_review")
	runID, err := db.createTerminalRun(ctx, "PROJECT-001", "TASK-001", "implementation", "succeeded", 1, "BASE", "HEAD", "DIFF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveRunArtifact(ctx, RunArtifactInput{
		ProjectID:    "PROJECT-001",
		RunID:        runID,
		ArtifactType: "diff",
		ArtifactKey:  "diff.patch",
		Content: []byte(`diff --git a/ui/src/App.tsx b/ui/src/App.tsx
index 1111111..2222222 100644
--- a/ui/src/App.tsx
+++ b/ui/src/App.tsx
@@ -1 +1 @@
-old
+new
diff --git a/internal/storage/review_repository.go b/internal/storage/review_repository.go
index 3333333..4444444 100644
--- a/internal/storage/review_repository.go
+++ b/internal/storage/review_repository.go
@@ -1 +1 @@
-old
+new
`),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := db.ReviewTask(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "ready_for_human_review" || result.ReviewRunID == "" {
		t.Fatalf("review result = %#v", result)
	}
	if result.ReviewArtifact.ID == "" || result.ReviewArtifact.ContentHash == "" {
		t.Fatalf("review artifact = %#v", result.ReviewArtifact)
	}
	if len(result.SemanticDiffs) != 2 {
		t.Fatalf("semantic diffs = %#v", result.SemanticDiffs)
	}
	categories := map[string]bool{}
	for _, diff := range result.SemanticDiffs {
		categories[diff.Category] = true
		if diff.Status != "ready" || diff.DiffArtifactID == "" || len(diff.Evidence) == 0 {
			t.Fatalf("semantic diff = %#v", diff)
		}
	}
	if !categories["user_visible"] || !categories["non_user_visible"] {
		t.Fatalf("categories = %#v", categories)
	}
	var diffCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM semantic_behavior_diffs WHERE task_id = 'TASK-001'").Scan(&diffCount); err != nil {
		t.Fatal(err)
	}
	if diffCount != 2 {
		t.Fatalf("diff count = %d", diffCount)
	}
}
