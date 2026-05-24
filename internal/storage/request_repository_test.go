package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCreateFeatureRequestCreatesQueueItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	result, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して")
	if err != nil {
		t.Fatal(err)
	}
	if result.FeatureRequest.Status != "queued" || result.FeatureRequest.Source != "human" {
		t.Fatalf("feature request = %#v", result.FeatureRequest)
	}
	if result.QueueItem.ItemType != "feature_request_analysis" || result.QueueItem.ItemID != result.FeatureRequest.ID {
		t.Fatalf("queue item = %#v", result.QueueItem)
	}

	requests, err := db.ListFeatureRequests(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].ID != result.FeatureRequest.ID {
		t.Fatalf("requests = %#v", requests)
	}
	items, err := db.ListWorkQueueItems(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != result.QueueItem.ID {
		t.Fatalf("queue items = %#v", items)
	}
}

func TestCreateFeatureRequestAllowsDuplicateBody(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	first, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "同じ要望")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "同じ要望")
	if err != nil {
		t.Fatal(err)
	}
	if first.FeatureRequest.ID == second.FeatureRequest.ID {
		t.Fatalf("duplicate body reused id: %s", first.FeatureRequest.ID)
	}
}

func TestCreateChangeRequestCreatesAnalysisQueueItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	result, err := db.CreateChangeRequest(ctx, "PROJECT-001", "タスク画面を今日中心に変える")
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangeRequest.Status != "proposed" || result.ChangeRequest.ID == "" {
		t.Fatalf("change request = %#v", result.ChangeRequest)
	}
	if result.QueueItem.ItemType != "change_request_analysis" || result.QueueItem.ItemID != result.ChangeRequest.ID {
		t.Fatalf("queue item = %#v", result.QueueItem)
	}
}

func TestAnalyzeChangeRequestCreatesImpactArtifact(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	created, err := db.CreateChangeRequest(ctx, "PROJECT-001", "タスク画面を今日中心に変える")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.AnalyzeChangeRequest(ctx, "PROJECT-001", created.ChangeRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangeRequest.Status != "impact_analyzed" {
		t.Fatalf("change request = %#v", result.ChangeRequest)
	}
	if result.Run.ChangeRequestID == nil || *result.Run.ChangeRequestID != created.ChangeRequest.ID {
		t.Fatalf("run = %#v", result.Run)
	}
	if result.Artifact.ArtifactType != "impact_analysis_report" || result.QueueItem == nil || result.QueueItem.Status != "completed" {
		t.Fatalf("analysis result = %#v", result)
	}
}

func TestApproveChangeRequestRequiresImpactAnalysis(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	created, err := db.CreateChangeRequest(ctx, "PROJECT-001", "タスク画面を今日中心に変える")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveChangeRequest(ctx, ChangeApproveInput{ProjectID: "PROJECT-001", ChangeRequestID: created.ChangeRequest.ID, Option: "A"}); err == nil {
		t.Fatal("expected approval before analysis to fail")
	}
	if _, err := db.AnalyzeChangeRequest(ctx, "PROJECT-001", created.ChangeRequest.ID); err != nil {
		t.Fatal(err)
	}
	approved, err := db.ApproveChangeRequest(ctx, ChangeApproveInput{ProjectID: "PROJECT-001", ChangeRequestID: created.ChangeRequest.ID, Option: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" {
		t.Fatalf("approved = %#v", approved)
	}
}

func TestStartPlanningCompletesFeatureRequestQueueItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	created, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して")
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.StartPlanning(ctx, PlanStartInput{ProjectID: "PROJECT-001", Concurrency: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StartedRuns) != 1 || result.StartedRuns[0].Status != "succeeded" {
		t.Fatalf("planning runs = %#v", result.StartedRuns)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].FeatureRequestID == nil || *result.Artifacts[0].FeatureRequestID != created.FeatureRequest.ID {
		t.Fatalf("planning artifacts = %#v", result.Artifacts)
	}
	if len(result.QueueItems) != 1 || result.QueueItems[0].Status != "completed" {
		t.Fatalf("queue items = %#v", result.QueueItems)
	}
	status, err := db.GetPlanningStatus(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Runs) != 1 || len(status.Artifacts) != 1 {
		t.Fatalf("planning status = %#v", status)
	}
}

func TestConsolidatePlanningCreatesTaskGroupProposal(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	created, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartPlanning(ctx, PlanStartInput{ProjectID: "PROJECT-001", Concurrency: 1}); err != nil {
		t.Fatal(err)
	}

	result, err := db.ConsolidatePlanning(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TaskGroups) != 1 || result.TaskGroups[0].FeatureRequestID == nil || *result.TaskGroups[0].FeatureRequestID != created.FeatureRequest.ID {
		t.Fatalf("task groups = %#v", result.TaskGroups)
	}
	if result.TaskGroups[0].Status != "proposed" || result.TaskGroups[0].PlanningUnit != "feature_chunk" {
		t.Fatalf("task group = %#v", result.TaskGroups[0])
	}
	if len(result.AcceptedArtifacts) != 1 || result.AcceptedArtifacts[0].Status != "accepted" {
		t.Fatalf("accepted artifacts = %#v", result.AcceptedArtifacts)
	}

	second, err := db.ConsolidatePlanning(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.TaskGroups) != 0 {
		t.Fatalf("second consolidation should be empty: %#v", second)
	}
}

func TestStartWorkProcessesPlanningAndConsolidation(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して"); err != nil {
		t.Fatal(err)
	}

	result, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		PlanningConcurrency:       2,
		ImplementationConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkerRun.Status != "stopped" {
		t.Fatalf("worker run = %#v", result.WorkerRun)
	}
	if len(result.Planning.StartedRuns) != 1 || len(result.Consolidation.TaskGroups) != 1 {
		t.Fatalf("work result = %#v", result)
	}
	status, err := db.GetWorkStatus(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.WorkerRuns) != 1 {
		t.Fatalf("work status = %#v", status)
	}
}

func TestStartWorkProcessesExecutionQueue(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", root, "approved")
	if _, err := db.MaterializeApprovedTasks(ctx, "PROJECT-001"); err != nil {
		t.Fatal(err)
	}

	result, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		PlanningConcurrency:       3,
		ImplementationConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Execution) != 1 {
		t.Fatalf("execution = %#v", result.Execution)
	}
	if result.Execution[0].QueueItem.Status != "completed" || result.Execution[0].Run.ImplementationRun == "" {
		t.Fatalf("execution result = %#v", result.Execution[0])
	}
}

func TestPauseAndResumeWorkerRun(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	running, err := db.createWorkerRun(ctx, "PROJECT-001", "planning", "bounded_parallel", 3)
	if err != nil {
		t.Fatal(err)
	}

	paused, err := db.PauseWorkerRun(ctx, "PROJECT-001", running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != "paused" {
		t.Fatalf("paused = %#v", paused)
	}
	pausedAgain, err := db.PauseWorkerRun(ctx, "PROJECT-001", running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pausedAgain.Status != "paused" {
		t.Fatalf("paused again = %#v", pausedAgain)
	}
	resumed, err := db.ResumeWorkerRun(ctx, "PROJECT-001", running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != "running" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestRecoverLostWorkQueueLeasesRequeuesOrFails(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	first, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "recover me")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "fail me")
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'running', lease_expires_at = ?, attempt_no = 1, max_attempts = 3
WHERE id = ?`, expired, first.QueueItem.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE work_queue_items
SET status = 'running', lease_expires_at = ?, attempt_no = 3, max_attempts = 3
WHERE id = ?`, expired, second.QueueItem.ID); err != nil {
		t.Fatal(err)
	}

	result, err := db.RecoverLostWorkQueueLeases(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recovered) != 1 || result.Recovered[0].ID != first.QueueItem.ID || result.Recovered[0].Status != "queued" {
		t.Fatalf("recovered = %#v", result.Recovered)
	}
	if len(result.Failed) != 1 || result.Failed[0].ID != second.QueueItem.ID || result.Failed[0].Status != "failed" {
		t.Fatalf("failed = %#v", result.Failed)
	}
}

func TestSaveEnvBindingStoresOnlyRedactedMetadata(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	record, err := db.SaveEnvBinding(ctx, EnvBindingInput{
		ProjectID: "PROJECT-001",
		Key:       "OPENAI_API_KEY",
		Scope:     "project",
		Value:     "secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "configured" || record.ValueFingerprint == "" || strings.Contains(record.StorageRef, "secret-value") {
		t.Fatalf("binding = %#v", record)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM environment_audit_events WHERE binding_id = ?", record.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit count = %d", count)
	}
}
