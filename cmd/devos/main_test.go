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

func ptr[T any](v T) *T {
	return &v
}
