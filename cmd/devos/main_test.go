package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/storage"
)

func TestPatchCLIWorkflow(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Patch workflow")
	seedPatchCLIApprovalEvidence(t, ctx, dataRoot, projectRoot)

	runCLI(t, "review", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	runCLI(t, "merge", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")

	exportOut := runCLI(t, "patch", "export", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	var exported storage.PatchApplicationRecord
	decodeJSON(t, exportOut, &exported)
	if exported.Status != "exported" {
		t.Fatalf("exported status = %s", exported.Status)
	}

	statusOut := runCLI(t, "patch", "status", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	var status struct {
		Patches []storage.PatchApplicationRecord `json:"patches"`
	}
	decodeJSON(t, statusOut, &status)
	if len(status.Patches) != 1 || status.Patches[0].ID != exported.ID {
		t.Fatalf("patch status = %#v", status.Patches)
	}

	appliedOut := runCLI(t, "patch", "mark-applied", "--project-root", projectRoot, "--data-root", dataRoot, "--commit", "COMMIT1", "--json", "TASK-001")
	var applied storage.PatchApplicationRecord
	decodeJSON(t, appliedOut, &applied)
	if applied.Status != "manually_applied" || applied.AppliedCommit != "COMMIT1" {
		t.Fatalf("applied patch = %#v", applied)
	}

	verifiedOut := runCLI(t, "patch", "verify-applied", "--project-root", projectRoot, "--data-root", dataRoot, "--adapter", "fake", "--json", "TASK-001")
	var verified storage.PatchApplicationRecord
	decodeJSON(t, verifiedOut, &verified)
	if verified.Status != "verified" {
		t.Fatalf("verified patch = %#v", verified)
	}

	cleanupOut := runCLI(t, "cleanup", "--project-root", projectRoot, "--data-root", dataRoot, "--applied", "--json")
	var cleanup struct {
		Items          []storage.CleanupPlanItem      `json:"items"`
		WorktreeSafety []storage.WorktreeSafetyRecord `json:"worktree_safety"`
	}
	decodeJSON(t, cleanupOut, &cleanup)
	if len(cleanup.Items) != 1 || cleanup.Items[0].TaskID != "TASK-001" {
		t.Fatalf("cleanup plan = %#v", cleanup.Items)
	}
	if len(cleanup.WorktreeSafety) != 1 || cleanup.WorktreeSafety[0].TaskID != "TASK-001" {
		t.Fatalf("worktree safety = %#v", cleanup.WorktreeSafety)
	}
}

func TestArtifactsListCLI(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Artifact list workflow")
	runCLI(t, "spec", "--project-root", projectRoot, "--data-root", dataRoot, "--json")
	out := runCLI(t, "artifacts", "--project-root", projectRoot, "--data-root", dataRoot, "--type", "prd", "--json")
	var result struct {
		Artifacts []storage.ArtifactRecord `json:"artifacts"`
	}
	decodeJSON(t, out, &result)
	if len(result.Artifacts) != 1 || result.Artifacts[0].ArtifactType != storage.ArtifactPRD {
		t.Fatalf("artifacts = %#v", result.Artifacts)
	}
}

func TestRequestQueueCLIWorkflow(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Request queue workflow")
	out := runCLI(t, "request", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Today Viewを追加して")
	var created storage.FeatureRequestCreateResult
	decodeJSON(t, out, &created)
	if created.FeatureRequest.ID == "" || created.FeatureRequest.Status != "queued" {
		t.Fatalf("feature request = %#v", created.FeatureRequest)
	}
	if created.QueueItem.ItemType != "feature_request_analysis" || created.QueueItem.ItemID != created.FeatureRequest.ID {
		t.Fatalf("queue item = %#v", created.QueueItem)
	}

	requestsOut := runCLI(t, "requests", "--project-root", projectRoot, "--data-root", dataRoot, "--status", "queued", "--json")
	var requests struct {
		FeatureRequests []storage.FeatureRequestRecord `json:"feature_requests"`
	}
	decodeJSON(t, requestsOut, &requests)
	if len(requests.FeatureRequests) != 1 || requests.FeatureRequests[0].ID != created.FeatureRequest.ID {
		t.Fatalf("feature requests = %#v", requests.FeatureRequests)
	}

	queueOut := runCLI(t, "queue", "--project-root", projectRoot, "--data-root", dataRoot, "--status", "queued", "--json")
	var queue struct {
		Items []storage.WorkQueueItemRecord `json:"items"`
	}
	decodeJSON(t, queueOut, &queue)
	if len(queue.Items) != 1 || queue.Items[0].ID != created.QueueItem.ID {
		t.Fatalf("queue = %#v", queue.Items)
	}
}

func TestChangeRequestCLIWorkflow(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Change request workflow")
	out := runCLI(t, "change", "request", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "タスク画面を今日中心に変える")
	var created storage.ChangeRequestCreateResult
	decodeJSON(t, out, &created)
	if created.ChangeRequest.Status != "proposed" || created.QueueItem.ItemType != "change_request_analysis" {
		t.Fatalf("change request = %#v", created)
	}
}

func TestPlanStartCLIWorkflow(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Plan start workflow")
	runCLI(t, "request", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Today Viewを追加して")
	out := runCLI(t, "plan", "start", "--project-root", projectRoot, "--data-root", dataRoot, "--concurrency", "2", "--json")
	var started storage.PlanStartResult
	decodeJSON(t, out, &started)
	if len(started.StartedRuns) != 1 || started.StartedRuns[0].Status != "succeeded" {
		t.Fatalf("started = %#v", started)
	}

	statusOut := runCLI(t, "plan", "status", "--project-root", projectRoot, "--data-root", dataRoot, "--json")
	var status storage.PlanningStatus
	decodeJSON(t, statusOut, &status)
	if len(status.Runs) != 1 || len(status.Artifacts) != 1 {
		t.Fatalf("planning status = %#v", status)
	}

	consolidateOut := runCLI(t, "plan", "consolidate", "--project-root", projectRoot, "--data-root", dataRoot, "--json")
	var consolidated storage.PlanConsolidateResult
	decodeJSON(t, consolidateOut, &consolidated)
	if len(consolidated.TaskGroups) != 1 || consolidated.TaskGroups[0].Status != "proposed" {
		t.Fatalf("consolidated = %#v", consolidated)
	}
}

func TestWorkStartCLIWorkflow(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Work start workflow")
	runCLI(t, "request", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Today Viewを追加して")
	out := runCLI(t, "work", "start", "--project-root", projectRoot, "--data-root", dataRoot, "--mode", "sequential", "--planning-concurrency", "2", "--implementation-concurrency", "1", "--json")
	var started storage.WorkStartResult
	decodeJSON(t, out, &started)
	if started.WorkerRun.Status != "stopped" || len(started.Consolidation.TaskGroups) != 1 {
		t.Fatalf("work start = %#v", started)
	}

	statusOut := runCLI(t, "work", "status", "--project-root", projectRoot, "--data-root", dataRoot, "--json")
	var status storage.WorkStatus
	decodeJSON(t, statusOut, &status)
	if len(status.WorkerRuns) != 1 {
		t.Fatalf("work status = %#v", status)
	}
}

func TestWorkPauseResumeCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Work pause workflow")
	workerID := insertCLIWorkerRun(t, ctx, dataRoot, projectRoot)
	pauseOut := runCLI(t, "work", "pause", "--project-root", projectRoot, "--data-root", dataRoot, "--json", workerID)
	var paused storage.WorkerRunRecord
	decodeJSON(t, pauseOut, &paused)
	if paused.Status != "paused" {
		t.Fatalf("paused = %#v", paused)
	}

	resumeOut := runCLI(t, "work", "resume", "--project-root", projectRoot, "--data-root", dataRoot, "--json", workerID)
	var resumed storage.WorkerRunRecord
	decodeJSON(t, resumeOut, &resumed)
	if resumed.Status != "running" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestReviewRejectCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Review reject workflow")
	seedPatchCLIApprovalEvidence(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "review", "reject", "--project-root", projectRoot, "--data-root", dataRoot, "--notes", "needs changes", "--json", "TASK-001")
	var result storage.ApprovalRecord
	decodeJSON(t, out, &result)
	if result.TaskStatus != "needs_decision" {
		t.Fatalf("review reject = %#v", result)
	}
}

func TestDecisionsListCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Decision list workflow")
	insertCLIDecision(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "decisions", "--project-root", projectRoot, "--data-root", dataRoot, "--status", "open", "--json")
	var result struct {
		Decisions []storage.DecisionRecord `json:"decisions"`
	}
	decodeJSON(t, out, &result)
	if len(result.Decisions) != 1 || result.Decisions[0].ID != "DEC-001" {
		t.Fatalf("decisions = %#v", result.Decisions)
	}
}

func TestApproveDecisionCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Decision approve workflow")
	insertCLIDecisionWithInbox(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--option", "A", "--notes", "recommended", "--json", "DEC-001")
	var result storage.DecisionRecord
	decodeJSON(t, out, &result)
	if result.Status != "approved" || result.SelectedOption != "A" {
		t.Fatalf("approved decision = %#v", result)
	}

	listOut := runCLI(t, "decisions", "--project-root", projectRoot, "--data-root", dataRoot, "--status", "approved", "--json")
	var list struct {
		Decisions []storage.DecisionRecord `json:"decisions"`
	}
	decodeJSON(t, listOut, &list)
	if len(list.Decisions) != 1 || list.Decisions[0].SelectedOption != "A" {
		t.Fatalf("approved decisions = %#v", list.Decisions)
	}
}

func TestInboxApproveDecisionCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Inbox approve workflow")
	insertCLIDecisionWithInbox(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "inbox", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--option", "A", "--json", "INBOX-DEC-001")
	var result storage.InboxApprovalResult
	decodeJSON(t, out, &result)
	if result.SourceType != "decision" || result.Decision == nil || result.Decision.Status != "approved" {
		t.Fatalf("inbox approval = %#v", result)
	}

	inboxOut := runCLI(t, "inbox", "--project-root", projectRoot, "--data-root", dataRoot, "--status", "open", "--json")
	var inbox struct {
		Items []storage.InboxItem `json:"items"`
	}
	decodeJSON(t, inboxOut, &inbox)
	for _, item := range inbox.Items {
		if item.SourceType == "decision" && item.SourceID == "DEC-001" {
			t.Fatalf("decision inbox remained open: %#v", item)
		}
	}
}

func TestInboxApproveHumanApprovalCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Inbox human approval workflow")
	seedPatchCLIApprovalEvidence(t, ctx, dataRoot, projectRoot)
	insertCLIOpenHumanApproval(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "inbox", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--notes", "approved", "--json", "INBOX-APPROVAL-FINAL")
	var result storage.InboxApprovalResult
	decodeJSON(t, out, &result)
	if result.SourceType != "human_approval" || result.HumanApproval == nil || result.HumanApproval.ID != "APPROVAL-FINAL" {
		t.Fatalf("inbox approval = %#v", result)
	}
}

func TestEnvStatusCLI(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Environment status workflow")
	out := runCLI(t, "env", "status", "--project-root", projectRoot, "--data-root", dataRoot, "--json")
	var result struct {
		Environments []struct {
			ID string `json:"id"`
		} `json:"environments"`
	}
	decodeJSON(t, out, &result)
	if len(result.Environments) != 1 || result.Environments[0].ID == "" {
		t.Fatalf("environments = %#v", result.Environments)
	}
}

func TestVerifyCLIWithFakeAdapter(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Verify workflow")
	seedCLIVerifyingTask(t, ctx, dataRoot, projectRoot)
	out := runCLI(t, "verify", "--project-root", projectRoot, "--data-root", dataRoot, "--adapter", "fake", "--json", "TASK-VERIFY")
	var result storage.VerifyTaskResult
	decodeJSON(t, out, &result)
	if result.TaskStatus != "ready_for_human_review" || result.VerificationRun == "" {
		t.Fatalf("verify result = %#v", result)
	}
}

func TestPlatformMapAddCLI(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Platform map workflow")
	runCLI(t, "platform", "profile", "set", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "hybrid")
	out := runCLI(t,
		"platform", "map", "add",
		"--project-root", projectRoot,
		"--data-root", dataRoot,
		"--from-root", "C:\\fake\\project",
		"--to-root", projectRoot,
		"--mode", "same_filesystem",
		"--write-owner", "windows-main",
		"--json",
		"windows-main", "wsl-sidecar",
	)
	var mapping storage.PathMappingRecord
	decodeJSON(t, out, &mapping)
	if mapping.Status != "active" || mapping.Mode != platform.MappingSameFilesystem || mapping.WriteOwnerEnvironmentID != "windows-main" {
		t.Fatalf("mapping = %#v", mapping)
	}
}

func TestPlatformDoctorSaveCLI(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Platform doctor save workflow")
	runCLI(t, "platform", "doctor", "--project-root", projectRoot, "--data-root", dataRoot, "--save", "--json")

	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM toolchain_requirements WHERE project_id = ?", projectID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected saved toolchain requirements")
	}
}

func TestPlatformDoctorEnvCLI(t *testing.T) {
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Platform doctor env workflow")
	runCLI(t, "platform", "profile", "set", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "wsl-primary")

	var stdout, stderr bytes.Buffer
	code := run([]string{"platform", "doctor", "--project-root", projectRoot, "--data-root", dataRoot, "--env", "wsl-main", "--json"}, &stdout, &stderr)
	if code != 0 && code != exitPolicy {
		t.Fatalf("unexpected doctor exit code %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	var report struct {
		EnvironmentID string `json:"environment_id"`
		Requirements  []struct {
			ToolchainKey string `json:"toolchain_key"`
		} `json:"requirements"`
	}
	decodeJSON(t, stdout.Bytes(), &report)
	if report.EnvironmentID != "wsl-main" {
		t.Fatalf("environment id = %s", report.EnvironmentID)
	}
	var foundWSL2 bool
	for _, req := range report.Requirements {
		if req.ToolchainKey == "wsl2" {
			foundWSL2 = true
		}
	}
	if !foundWSL2 {
		t.Fatalf("wsl2 requirement not found: %#v", report.Requirements)
	}
}

func TestPlatformSetupInstructionsAndMarkInstalledCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Platform setup workflow")
	runCLI(t, "platform", "doctor", "--project-root", projectRoot, "--data-root", dataRoot, "--save", "--json")
	inboxID := insertCLIToolchainSetupForDetectedGit(t, ctx, dataRoot, projectRoot)
	instructionsOut := runCLI(t, "platform", "setup", "instructions", "--project-root", projectRoot, "--data-root", dataRoot, "--json", inboxID)
	var instructions storage.ToolchainSetupInstructions
	decodeJSON(t, instructionsOut, &instructions)
	if instructions.InboxID != inboxID || instructions.ToolchainKey != "git" || len(instructions.Instructions) == 0 {
		t.Fatalf("instructions = %#v", instructions)
	}

	markOut := runCLI(t, "platform", "setup", "mark-installed", "--project-root", projectRoot, "--data-root", dataRoot, "--json", inboxID)
	var mark struct {
		Resolved bool `json:"resolved"`
	}
	decodeJSON(t, markOut, &mark)
	if !mark.Resolved {
		t.Fatalf("mark installed result = %#v", mark)
	}
}

func TestPlatformSetupWaiveCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Platform setup waive workflow")
	runCLI(t, "platform", "doctor", "--project-root", projectRoot, "--data-root", dataRoot, "--save", "--json")

	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	var inboxID string
	if err := db.SQL().QueryRowContext(ctx, `
SELECT ii.id
FROM inbox_items ii
JOIN toolchain_requirements tr ON tr.id = ii.source_id
WHERE ii.project_id = ? AND ii.item_type = 'toolchain_setup' AND tr.toolchain_key = 'bubblewrap'
LIMIT 1`, projectID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t,
		"platform", "setup", "waive",
		"--project-root", projectRoot,
		"--data-root", dataRoot,
		"--reason", "not needed for local smoke",
		"--scope", "local-only",
		"--expiry", "2026-06-01T00:00:00Z",
		"--allowed-effect", "report_only",
		"--json",
		inboxID,
	)
	var waiver storage.ToolchainWaiverRecord
	decodeJSON(t, out, &waiver)
	if waiver.InboxID != inboxID || waiver.Status != "waived" || waiver.AllowedEffect != "report_only" {
		t.Fatalf("waiver = %#v", waiver)
	}
}

func TestMergeQueueSimulateConflictCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Merge conflict workflow")
	seedPatchCLIApprovalEvidence(t, ctx, dataRoot, projectRoot)

	runCLI(t, "review", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	runCLI(t, "merge", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	dryRunOut := runCLI(t, "merge", "--project-root", projectRoot, "--data-root", dataRoot, "--dry-run", "--json", "TASK-001")
	var dryRun storage.MergeQueueEntry
	decodeJSON(t, dryRunOut, &dryRun)
	if dryRun.Status != "queued" {
		t.Fatalf("dry-run merge = %#v", dryRun)
	}
	runCLI(t, "merge", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")

	conflictOut := runCLI(t, "merge", "queue", "--project-root", projectRoot, "--data-root", dataRoot, "--process-fake", "--simulate-conflict", "--conflict-reason", "conflict in fake.txt", "--json")
	var conflict storage.FakeMergeResult
	decodeJSON(t, conflictOut, &conflict)
	if conflict.TaskStatus != "merge_conflict" {
		t.Fatalf("conflict result = %#v", conflict)
	}
	retryOut := runCLI(t, "merge", "queue", "--project-root", projectRoot, "--data-root", dataRoot, "--retry-conflict", conflict.MergeQueueEntryID, "--json")
	var retry storage.FakeMergeResult
	decodeJSON(t, retryOut, &retry)
	if retry.TaskStatus != "merged" {
		t.Fatalf("retry result = %#v", retry)
	}
}

func TestMergeQueueRealGitDryRunCLI(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	dataRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	gitRun(t, projectRoot, "config", "user.email", "test@example.com")
	gitRun(t, projectRoot, "config", "user.name", "Test User")
	gitRun(t, projectRoot, "add", ".gitignore")
	gitRun(t, projectRoot, "commit", "-m", "initial")

	runCLI(t, "init", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "Real git dry-run workflow")
	gitRun(t, projectRoot, "add", ".devagent")
	gitRun(t, projectRoot, "commit", "-m", "devagent init")
	head := gitOutput(t, projectRoot, "rev-parse", "HEAD")
	seedPatchCLIApprovalEvidence(t, ctx, dataRoot, projectRoot)
	updateCLIRunEvidence(t, ctx, dataRoot, projectRoot, head, head)

	runCLI(t, "review", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	runCLI(t, "merge", "approve", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	queueOut := runCLI(t, "merge", "--project-root", projectRoot, "--data-root", dataRoot, "--json", "TASK-001")
	var queue storage.MergeQueueEntry
	decodeJSON(t, queueOut, &queue)

	gitDryRunOut := runCLI(t, "merge", "queue", "--project-root", projectRoot, "--data-root", dataRoot, "--dry-run-real-git", "--entry", queue.ID, "--json")
	var result storage.GitDryRunResult
	decodeJSON(t, gitDryRunOut, &result)
	if result.Status != "succeeded" || result.Classification != "clean" || len(result.Blockers) != 0 {
		t.Fatalf("real git dry-run = %#v", result)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".env.*\n.devagent-worktrees/\norchestrator-data/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(out))
	}
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

func runCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("devos %v failed with %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}

func decodeJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode JSON failed: %v\n%s", err, string(raw))
	}
}

func seedPatchCLIApprovalEvidence(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var environmentID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM execution_environments WHERE project_id = ? AND role = 'primary'", projectID).Scan(&environmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO tasks(
  id, project_id, status, title, base_branch, created_at, updated_at
) VALUES ('TASK-001', ?, 'ready_for_human_review', 'Patch CLI workflow', 'main', ?, ?)`,
		projectID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit, head_commit,
  diff_hash, created_at, updated_at, started_at, completed_at
) VALUES ('RUN-001', ?, 'TASK-001', 'verification', 'succeeded', 1, 'BASE', 'HEAD', 'DIFF', ?, ?, ?, ?)`,
		projectID, now, now, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO verification_results(
  id, project_id, run_id, environment_id, command_id, required_for_merge,
  status, evidence_json, created_at
) VALUES ('VERIF-001', ?, 'RUN-001', ?, 'go-test', 1, 'passed', '{}', ?)`,
		projectID, environmentID, now,
	); err != nil {
		t.Fatal(err)
	}
	gates := []decisions.GateResult{{
		Status:   decisions.GatePass,
		Severity: decisions.SeverityLow,
		Detector: "verification_passed",
		Evidence: map[string]any{"run_id": "RUN-001"},
	}}
	if err := db.SaveGateResults(ctx, projectID, ptr("TASK-001"), "RUN-001", gates); err != nil {
		t.Fatal(err)
	}
}

func seedCLIVerifyingTask(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO tasks(
  id, project_id, status, title, base_branch, created_at, updated_at
) VALUES ('TASK-VERIFY', ?, 'verifying', 'Verify CLI workflow', 'main', ?, ?)`,
		projectID, now, now,
	); err != nil {
		t.Fatal(err)
	}
}

func updateCLIRunEvidence(t *testing.T, ctx context.Context, dataRoot string, projectRoot string, baseCommit string, headCommit string) {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	if _, err := db.SQL().ExecContext(ctx, "UPDATE runs SET base_commit = ?, head_commit = ? WHERE project_id = ? AND id = 'RUN-001'", baseCommit, headCommit, projectID); err != nil {
		t.Fatal(err)
	}
}

func insertCLIDecision(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES ('DEC-001', ?, 'open', 'Choose behavior', '[]', '{}', ?, ?)`, projectID, now, now); err != nil {
		t.Fatal(err)
	}
}

func insertCLIDecisionWithInbox(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) {
	t.Helper()
	insertCLIDecision(t, ctx, dataRoot, projectRoot)
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  priority, title, body, created_at, updated_at
) VALUES ('INBOX-DEC-001', ?, 'human_decision', 'open', 'decision', 'DEC-001', 'decision-DEC-001', 10, 'Choose behavior', 'Pick an option', ?, ?)`,
		projectID, now, now); err != nil {
		t.Fatal(err)
	}
}

func insertCLIOpenHumanApproval(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO human_approvals(
  id, project_id, task_id, approval_type, status, evidence_json, created_at, updated_at
) VALUES ('APPROVAL-FINAL', ?, 'TASK-001', 'final_review', 'open', '{}', ?, ?)`,
		projectID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id, dedupe_key,
  priority, title, body, created_at, updated_at
) VALUES ('INBOX-APPROVAL-FINAL', ?, 'TASK-001', 'approval', 'open', 'human_approval', 'APPROVAL-FINAL', 'human-approval-final', 70, 'Approval required', 'Approve final review', ?, ?)`,
		projectID, now, now); err != nil {
		t.Fatal(err)
	}
}

func insertCLIToolchainSetupForDetectedGit(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) string {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	var requirementID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM toolchain_requirements WHERE project_id = ? AND toolchain_key = 'git' AND status = 'detected' LIMIT 1", projectID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	inboxID := "INBOX-DETECTED-GIT"
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  priority, title, body, created_at, updated_at
) VALUES (?, ?, 'toolchain_setup', 'open', 'toolchain_requirement', ?, 'detected-git-test', 40, 'Toolchain setup required: git', 'git setup check', ?, ?)`,
		inboxID, projectID, requirementID, now, now); err != nil {
		t.Fatal(err)
	}
	return inboxID
}

func insertCLIWorkerRun(t *testing.T, ctx context.Context, dataRoot string, projectRoot string) string {
	t.Helper()
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projectID := storage.ProjectIDForRoot(projectRoot)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	workerID := "WORKER-CLI-001"
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO worker_runs(
  id, project_id, lane, mode, max_concurrency, status,
  started_at, lease_owner, last_heartbeat_at
) VALUES (?, ?, 'planning', 'bounded_parallel', 3, 'running', ?, 'test', ?)`,
		workerID, projectID, now, now); err != nil {
		t.Fatal(err)
	}
	return workerID
}

func ptr[T any](v T) *T {
	return &v
}
