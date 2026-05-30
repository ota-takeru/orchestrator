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
	requests, err := db.ListFeatureRequests(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].ChangeRequestID == nil || *requests[0].ChangeRequestID != created.ChangeRequest.ID {
		t.Fatalf("change request feature queue = %#v", requests)
	}
}

func TestStartPlanningCompletesFeatureRequestQueueItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	created, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "UX layout directionを変えたい")
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
	if _, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "UX layout directionを変えたい"); err != nil {
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
	created, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "UX layout directionを変えたい")
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
	decisions, err := db.ListDecisions(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 0 {
		t.Fatalf("planning decisions = %#v", decisions)
	}
	packets, err := db.ListApprovalPackets(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].Summary.TaskID != result.ProposedTasks[0].ID || packets[0].RiskLevel != "L2" {
		t.Fatalf("approval packets = %#v", packets)
	}
	if _, err := db.ApproveApprovalPacket(ctx, ApprovalPacketApprovalInput{
		ProjectID: "PROJECT-001",
		PacketID:  packets[0].ID,
		Option:    "approve_recommended",
	}); err != nil {
		t.Fatal(err)
	}
	readyTasks, err := db.ListTasks(ctx, "PROJECT-001", "ready")
	if err != nil {
		t.Fatal(err)
	}
	if len(readyTasks) != 1 || readyTasks[0].ID != result.ProposedTasks[0].ID {
		t.Fatalf("ready tasks = %#v", readyTasks)
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
	if _, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "UX layout directionを変えたい"); err != nil {
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

func TestStartWorkProcessesExecutionQueueWithRealCodexAdapter(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/devos-worker\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "/tmp/devos-codex-home")
	setRealCodexDoctorDetectedForTest(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", root)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, created_at, updated_at
) VALUES ('WQ-REAL', 'PROJECT-001', 'execution', 'task_implementation', 'TASK-001', 'queued', 'medium', 0, 3, 'task_implementation:TASK-001', ?, ?)`,
		now(), now()); err != nil {
		t.Fatal(err)
	}

	result, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		ImplementationAdapter:     "real-codex",
		PlanningConcurrency:       1,
		ImplementationConcurrency: 1,
		CodexExecutor: fakeCodexExecutor{result: CodexExecResult{
			Stdout:       "{\"type\":\"done\"}\n",
			FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`,
			ExitCode:     0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Execution) != 1 || result.Execution[0].RealRun == nil || result.Execution[0].Verification == nil {
		t.Fatalf("execution = %#v", result.Execution)
	}
	if result.Execution[0].TaskStatus != "ready_for_human_review" {
		t.Fatalf("task status = %s", result.Execution[0].TaskStatus)
	}
}

func TestStartWorkProcessesRepairQueueWithRealCodexAdapter(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/devos-repair\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "/tmp/devos-codex-home")
	setRealCodexDoctorDetectedForTest(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", root)
	insertTask(t, db, "PROJECT-001", "TASK-001", "repairing")
	if _, err := db.EnqueueTaskRepair(ctx, "PROJECT-001", "TASK-001", "RUN-FAILED"); err != nil {
		t.Fatal(err)
	}

	result, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		ImplementationAdapter:     "real-codex",
		PlanningConcurrency:       1,
		ImplementationConcurrency: 1,
		CodexExecutor: fakeCodexExecutor{result: CodexExecResult{
			Stdout:       "{\"type\":\"done\"}\n",
			FinalMessage: `{"status":"succeeded","summary":"repair done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`,
			ExitCode:     0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Execution) != 1 || result.Execution[0].RealRun == nil || result.Execution[0].RealRun.RepairRun == "" || result.Execution[0].Verification == nil {
		t.Fatalf("execution = %#v", result.Execution)
	}
	if result.Execution[0].TaskStatus != "ready_for_human_review" {
		t.Fatalf("task status = %s", result.Execution[0].TaskStatus)
	}
	var runType string
	if err := db.SQL().QueryRowContext(ctx, "SELECT run_type FROM runs WHERE id = ?", result.Execution[0].RealRun.RepairRun).Scan(&runType); err != nil {
		t.Fatal(err)
	}
	if runType != "repair" {
		t.Fatalf("run type = %s", runType)
	}
}

func TestStartWorkCompletesStaleExecutionQueueItems(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
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

	if _, err := db.StartWork(ctx, WorkStartInput{
		ProjectID:                 "PROJECT-001",
		Mode:                      "sequential",
		PlanningConcurrency:       3,
		ImplementationConcurrency: 1,
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := db.ListWorkQueueItems(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 0 {
		t.Fatalf("queued items = %#v", queued)
	}
	completed, err := db.ListWorkQueueItems(ctx, "PROJECT-001", "completed")
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 || completed[0].ItemID != tasks[0].ID {
		t.Fatalf("completed items = %#v", completed)
	}
}

func TestStartWorkRequeuesReadyTaskWithTerminalExecutionItem(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, finished_at, created_at, updated_at
) VALUES (
  'WQ-55E6A7FFE4CB', 'PROJECT-001', 'execution', 'task_implementation',
  'TASK-001', 'completed', 'medium', 1, 3, 'task_implementation:TASK-001',
  ?, ?, ?
)`, now(), now(), now()); err != nil {
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
	if result.Execution[0].QueueItem.Status != "completed" || result.Execution[0].TaskStatus != "ready_for_human_review" {
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
	if record.ValueFingerprint == sha256Hex([]byte("secret-value")) {
		t.Fatal("fingerprint must not be raw sha256 of the secret value")
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

	replaced, err := db.SaveEnvBinding(ctx, EnvBindingInput{
		ProjectID: "PROJECT-001",
		Key:       "OPENAI_API_KEY",
		Scope:     "project",
		Value:     "new-secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != record.ID {
		t.Fatalf("expected duplicate key to replace same binding: %s != %s", replaced.ID, record.ID)
	}
	envLocal, err = os.ReadFile(filepath.Join(root, ".env.local"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(envLocal)
	if strings.Contains(content, "OPENAI_API_KEY=secret-value\n") || strings.Count(content, "OPENAI_API_KEY=") != 1 || !strings.Contains(content, "OPENAI_API_KEY=new-secret-value\n") {
		t.Fatalf(".env.local duplicate replacement failed: %q", content)
	}
}

func TestSaveEnvBindingResolvesRequirementAndRequeuesTask(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := t.TempDir()
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertTask(t, db, "PROJECT-001", "TASK-001", "needs_input")
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO environment_requirements(
  id, project_id, key, required_for, status, source_hint, validation_json, description, created_at
) VALUES ('ENVREQ-001', 'PROJECT-001', 'OPENAI_API_KEY', 'runtime', 'requested', 'user_input', '{}', 'api key', ?)`,
		now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES ('INBOX-ENV', 'PROJECT-001', 'TASK-001', 'human_input', 'open', 'environment_requirement', 'ENVREQ-001', 'env:OPENAI_API_KEY', 80, 'Need env', 'OPENAI_API_KEY', ?, ?)`,
		now(), now()); err != nil {
		t.Fatal(err)
	}

	if _, err := db.SaveEnvBinding(ctx, EnvBindingInput{
		ProjectID: "PROJECT-001",
		Key:       "OPENAI_API_KEY",
		Scope:     "task",
		ScopeID:   "TASK-001",
		Value:     "secret-value",
	}); err != nil {
		t.Fatal(err)
	}
	var requirementStatus, inboxStatus, taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM environment_requirements WHERE id = 'ENVREQ-001'").Scan(&requirementStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM inbox_items WHERE id = 'INBOX-ENV'").Scan(&inboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if requirementStatus != "configured" || inboxStatus != "resolved" || taskStatus != "ready" {
		t.Fatalf("requirement=%s inbox=%s task=%s", requirementStatus, inboxStatus, taskStatus)
	}
	items, err := db.ListWorkQueueItems(ctx, "PROJECT-001", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ItemType != "task_implementation" || items[0].ItemID != "TASK-001" {
		t.Fatalf("queue = %#v", items)
	}
}

func TestSaveEnvBindingRefusesTrackedEnvLocal(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := t.TempDir()
	if err := runGit(ctx, root, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("EXISTING=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, root, "add", "--", ".env.local"); err != nil {
		t.Fatal(err)
	}
	insertProjectWithRoot(t, db, "PROJECT-001", root)

	_, err := db.SaveEnvBinding(ctx, EnvBindingInput{
		ProjectID: "PROJECT-001",
		Key:       "OPENAI_API_KEY",
		Scope:     "project",
		Value:     "secret-value",
	})
	if err == nil || !strings.Contains(err.Error(), "tracked by git") {
		t.Fatalf("err = %v", err)
	}
}
