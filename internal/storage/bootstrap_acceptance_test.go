package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestBootstrapFakeTaskMerges(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", root, "approved")
	tasks, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunFakeTask(ctx, "PROJECT-001", tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: tasks[0].ID, ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: tasks[0].ID, ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueueTaskForMerge(ctx, "PROJECT-001", tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProcessNextFakeMerge(ctx, "PROJECT-001"); err != nil {
		t.Fatal(err)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ?", tasks[0].ID).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "merged" {
		t.Fatalf("task status = %s", taskStatus)
	}
}

func TestWindowsPrimaryFakeBootstrapPasses(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	if _, err := db.ConfigureFakeRunProfile(ctx, "PROJECT-001", platform.PlatformModeWindowsPrimary, root); err != nil {
		t.Fatal(err)
	}
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", root, "approved")
	tasks, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunFakeTask(ctx, "PROJECT-001", tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: tasks[0].ID, ApprovalType: ApprovalFinalReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, ApprovalInput{ProjectID: "PROJECT-001", TaskID: tasks[0].ID, ApprovalType: ApprovalMerge}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueueTaskForMerge(ctx, "PROJECT-001", tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProcessNextFakeMerge(ctx, "PROJECT-001"); err != nil {
		t.Fatal(err)
	}
	var osFamily string
	if err := db.SQL().QueryRowContext(ctx, "SELECT os_family FROM execution_environments WHERE project_id = 'PROJECT-001' AND role = 'primary'").Scan(&osFamily); err != nil {
		t.Fatal(err)
	}
	if osFamily != "windows" {
		t.Fatalf("primary os family = %s", osFamily)
	}
}
