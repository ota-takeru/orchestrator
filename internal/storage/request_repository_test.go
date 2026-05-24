package storage

import (
	"context"
	"os"
	"path/filepath"
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

func TestChangeRequestUsesTraceLinks(t *testing.T) {
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
	var runLinks, artifactLinks int
	if err := db.SQL().QueryRowContext(ctx, `
SELECT COUNT(*) FROM trace_links
WHERE project_id = 'PROJECT-001'
  AND from_type = 'change_request'
  AND from_id = ?
  AND to_type = 'planning_run'
  AND to_id = ?
  AND relation = 'analyzed_by'`, created.ChangeRequest.ID, result.Run.ID).Scan(&runLinks); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `
SELECT COUNT(*) FROM trace_links
WHERE project_id = 'PROJECT-001'
  AND from_type = 'change_request'
  AND from_id = ?
  AND to_type = 'planning_artifact'
  AND to_id = ?
  AND relation = 'produced'`, created.ChangeRequest.ID, result.Artifact.ID).Scan(&artifactLinks); err != nil {
		t.Fatal(err)
	}
	if runLinks != 1 || artifactLinks != 1 {
		t.Fatalf("trace links run=%d artifact=%d", runLinks, artifactLinks)
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
	if len(result.StartedRuns) != 5 || result.StartedRuns[0].Status != "succeeded" {
		t.Fatalf("planning runs = %#v", result.StartedRuns)
	}
	if len(result.Artifacts) != 4 || result.Artifacts[0].FeatureRequestID == nil || *result.Artifacts[0].FeatureRequestID != created.FeatureRequest.ID {
		t.Fatalf("planning artifacts = %#v", result.Artifacts)
	}
	if len(result.DecisionDrafts) != 1 || result.DecisionDrafts[0].Status != "draft" {
		t.Fatalf("decision drafts = %#v", result.DecisionDrafts)
	}
	if len(result.QueueItems) != 1 || result.QueueItems[0].Status != "completed" {
		t.Fatalf("queue items = %#v", result.QueueItems)
	}
	status, err := db.GetPlanningStatus(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Runs) != 5 || len(status.Artifacts) != 4 {
		t.Fatalf("planning status = %#v", status)
	}
}

func TestPlanningWorkerCannotWriteCanonicalArtifacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Today Viewを追加して"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.StartPlanning(ctx, PlanStartInput{ProjectID: "PROJECT-001", Concurrency: 1}); err != nil {
		t.Fatal(err)
	}
	var taskCount, taskGroupCount, artifactVersionCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE project_id = 'PROJECT-001'").Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_groups WHERE project_id = 'PROJECT-001'").Scan(&taskGroupCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM artifact_versions av JOIN artifacts a ON a.id = av.artifact_id WHERE a.project_id = 'PROJECT-001'").Scan(&artifactVersionCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || taskGroupCount != 0 || artifactVersionCount != 0 {
		t.Fatalf("planning worker wrote canonical state: tasks=%d groups=%d versions=%d", taskCount, taskGroupCount, artifactVersionCount)
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
	if len(result.ProposedTasks) != 1 || result.ProposedTasks[0].Status != "proposed" {
		t.Fatalf("proposed tasks = %#v", result.ProposedTasks)
	}
	var batchedDrafts int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM decision_report_drafts WHERE project_id = 'PROJECT-001' AND status = 'batched'").Scan(&batchedDrafts); err != nil {
		t.Fatal(err)
	}
	if batchedDrafts != 1 {
		t.Fatalf("batched drafts = %d", batchedDrafts)
	}
	checkpoint, err := db.CreateRollingCheckpoint(ctx, RollingCheckpointInput{
		ProjectID: "PROJECT-001",
		TaskID:    result.ProposedTasks[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Run.RunType != "rolling_checkpoint" || checkpoint.Run.Status != "succeeded" {
		t.Fatalf("checkpoint run = %#v", checkpoint.Run)
	}
	if checkpoint.Artifact.ArtifactType != "rolling_checkpoint_report" || checkpoint.Artifact.Status != "proposed" {
		t.Fatalf("checkpoint artifact = %#v", checkpoint.Artifact)
	}
	if checkpoint.Snapshot.Task.ID != result.ProposedTasks[0].ID || checkpoint.Snapshot.NextAction != "review_task_proposal" {
		t.Fatalf("checkpoint snapshot = %#v", checkpoint.Snapshot)
	}
	secondCheckpoint, err := db.CreateRollingCheckpoint(ctx, RollingCheckpointInput{
		ProjectID: "PROJECT-001",
		TaskID:    result.ProposedTasks[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondCheckpoint.Run.ID != checkpoint.Run.ID || secondCheckpoint.Artifact.ID != checkpoint.Artifact.ID {
		t.Fatalf("checkpoint should be idempotent: first=%#v second=%#v", checkpoint, secondCheckpoint)
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

func TestStalePlanningSnapshotCannotCommit(t *testing.T) {
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
	if _, err := db.SQL().ExecContext(ctx, "UPDATE feature_requests SET description = 'updated request', updated_at = ? WHERE id = ?", now(), created.FeatureRequest.ID); err != nil {
		t.Fatal(err)
	}

	result, err := db.ConsolidatePlanning(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TaskGroups) != 0 || len(result.ProposedTasks) != 0 || len(result.AcceptedArtifacts) != 1 || result.AcceptedArtifacts[0].Status != "stale" {
		t.Fatalf("consolidation should reject stale snapshot: %#v", result)
	}
	var staleArtifacts, taskCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM planning_artifacts WHERE project_id = 'PROJECT-001' AND status = 'stale'").Scan(&staleArtifacts); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE project_id = 'PROJECT-001'").Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if staleArtifacts != 1 || taskCount != 0 {
		t.Fatalf("stale artifacts=%d task count=%d", staleArtifacts, taskCount)
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
	if len(result.Planning.StartedRuns) != 5 || len(result.Consolidation.TaskGroups) != 1 {
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

func TestStartWorkProcessesQueuedRepair(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", t.TempDir())
	insertTask(t, db, "PROJECT-001", "TASK-001", "repairing")
	if _, err := db.EnqueueTaskRepair(ctx, "PROJECT-001", "TASK-001", "RUN-FAILED"); err != nil {
		t.Fatal(err)
	}

	result, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		PlanningConcurrency:       1,
		ImplementationConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Execution) != 1 || result.Execution[0].TaskStatus != "ready_for_human_review" || result.Execution[0].Run.RepairRun == "" {
		t.Fatalf("work result = %#v", result)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "ready_for_human_review" {
		t.Fatalf("task status = %s", taskStatus)
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
	root := t.TempDir()
	insertProjectWithRoot(t, db, "PROJECT-001", root)

	record, err := db.SaveEnvBinding(ctx, EnvBindingInput{
		ProjectID: "PROJECT-001",
		Key:       "OPENAI_API_KEY",
		Scope:     "project",
		Value:     "secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "configured" || record.ValueFingerprint == "" || record.Storage != "env_file" || strings.Contains(record.StorageRef, "secret-value") {
		t.Fatalf("binding = %#v", record)
	}
	envLocal, err := os.ReadFile(filepath.Join(root, ".env.local"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envLocal), "OPENAI_API_KEY=secret-value\n") {
		t.Fatalf(".env.local = %q", string(envLocal))
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM environment_audit_events WHERE binding_id = ?", record.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit count = %d", count)
	}
}
