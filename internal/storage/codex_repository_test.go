package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

type fakeCodexExecutor struct {
	result CodexExecResult
}

func (f fakeCodexExecutor) ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error) {
	_ = ctx
	if f.result.ExitCode == 0 {
		_ = os.WriteFile(filepath.Join(request.ProjectRoot, "codex-output.txt"), []byte("implemented\n"), 0o644)
	}
	if f.result.StartedAt.IsZero() {
		f.result.StartedAt = time.Now().UTC()
	}
	if f.result.CompletedAt.IsZero() {
		f.result.CompletedAt = f.result.StartedAt.Add(time.Second)
	}
	return f.result, nil
}

type captureCodexExecutor struct {
	request CodexExecRequest
	result  CodexExecResult
}

func (f *captureCodexExecutor) ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error) {
	_ = ctx
	f.request = request
	if f.result.ExitCode == 0 {
		_ = os.WriteFile(filepath.Join(request.ProjectRoot, "codex-output.txt"), []byte("implemented\n"), 0o644)
	}
	if f.result.StartedAt.IsZero() {
		f.result.StartedAt = time.Now().UTC()
	}
	if f.result.CompletedAt.IsZero() {
		f.result.CompletedAt = f.result.StartedAt.Add(time.Second)
	}
	return f.result, nil
}

type noChangeCodexExecutor struct {
	result CodexExecResult
}

func (f noChangeCodexExecutor) ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error) {
	_ = ctx
	_ = request
	if f.result.StartedAt.IsZero() {
		f.result.StartedAt = time.Now().UTC()
	}
	if f.result.CompletedAt.IsZero() {
		f.result.CompletedAt = f.result.StartedAt.Add(time.Second)
	}
	return f.result, nil
}

func TestRunRealCodexTaskRecordsImplementationEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	t.Setenv("CODEX_HOME", "/tmp/devos-codex-home")
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "verifying" || result.Classification != "succeeded" {
		t.Fatalf("result = %#v", result)
	}
	var runStatus, taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = ?", result.ImplementationRun).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "succeeded" || taskStatus != "verifying" {
		t.Fatalf("run=%s task=%s", runStatus, taskStatus)
	}
	var artifactCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM run_artifacts WHERE run_id = ?", result.ImplementationRun).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if artifactCount < 4 {
		t.Fatalf("artifact count = %d", artifactCount)
	}
	var summaryPath string
	if err := db.SQL().QueryRowContext(ctx, "SELECT path FROM run_artifacts WHERE run_id = ? AND artifact_key = 'real-codex-summary.json'", result.ImplementationRun).Scan(&summaryPath); err != nil {
		t.Fatal(err)
	}
	rawSummary, err := os.ReadFile(filepath.Join(db.dataRoot, summaryPath))
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		EnvironmentID   string `json:"environment_id"`
		CodexAdapter    string `json:"codex_adapter"`
		SandboxProfile  string `json:"sandbox_profile"`
		CodexHomeSource string `json:"codex_home_source"`
	}
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.EnvironmentID != "linux-main" || summary.CodexAdapter != "codex-linux" || summary.SandboxProfile != "linux-bubblewrap" || summary.CodexHomeSource != "CODEX_HOME" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestRunRealCodexTaskUsesTaskWorktreeWhenGitRepo(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := initStorageGitRepo(t)
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	executor := &captureCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	}

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", executor)
	if err != nil {
		t.Fatal(err)
	}
	expectedWorktree := filepath.Join(projectRoot, ".devagent-worktrees", "TASK-001")
	if executor.request.ProjectRoot != expectedWorktree || result.WorktreeRoot != expectedWorktree {
		t.Fatalf("executor root=%s result root=%s expected=%s", executor.request.ProjectRoot, result.WorktreeRoot, expectedWorktree)
	}
	if strings.TrimSpace(gitOutput(t, expectedWorktree, "rev-parse", "--is-inside-work-tree")) != "true" {
		t.Fatalf("expected task worktree at %s", expectedWorktree)
	}
	var cwd string
	if err := db.SQL().QueryRowContext(ctx, "SELECT cwd FROM command_events WHERE run_id = ? AND command_kind = 'codex'", result.ImplementationRun).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	if cwd != expectedWorktree {
		t.Fatalf("command cwd = %s", cwd)
	}
	if strings.TrimSpace(result.HeadCommit) == "" || result.HeadCommit == "UNKNOWN" {
		t.Fatalf("head commit was not recorded: %#v", result)
	}
	if strings.TrimSpace(gitOutput(t, expectedWorktree, "diff", "--name-only", "HEAD^", "HEAD")) != "codex-output.txt" {
		t.Fatalf("expected committed Codex change in task worktree")
	}
	var headCommit string
	if err := db.SQL().QueryRowContext(ctx, "SELECT head_commit FROM runs WHERE id = ?", result.ImplementationRun).Scan(&headCommit); err != nil {
		t.Fatal(err)
	}
	if headCommit != result.HeadCommit {
		t.Fatalf("stored head commit = %s result = %s", headCommit, result.HeadCommit)
	}
}

func TestRunRealCodexTaskNoChangeBlocks(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := initStorageGitRepo(t)
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", noChangeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "no_change" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunRealCodexTaskDirtyCanonicalWorktreeOpensDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := initStorageGitRepo(t)
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	if err := os.WriteFile(filepath.Join(projectRoot, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "worktree_required" {
		t.Fatalf("result = %#v", result)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "needs_decision" {
		t.Fatalf("task status = %s", taskStatus)
	}
}

func TestRunRealCodexTaskIncludesTrustedArtifactContext(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	approved, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\napproved"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	notes := "Keep Human Inbox in the first workflow."
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", approved.ArtifactID, approved.Version, "approved_with_notes", notes); err != nil {
		t.Fatal(err)
	}
	proposed, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\nunapproved proposal"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &captureCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	}

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", executor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(executor.request.Prompt, "Trusted artifact context:") ||
		!strings.Contains(executor.request.Prompt, "version=1") ||
		!strings.Contains(executor.request.Prompt, "status=approved_with_notes") ||
		!strings.Contains(executor.request.Prompt, approved.Hash) ||
		!strings.Contains(executor.request.Prompt, "approval_notes: "+notes) ||
		!strings.Contains(executor.request.Prompt, "    # PRD") ||
		!strings.Contains(executor.request.Prompt, "    approved") {
		t.Fatalf("prompt did not include trusted artifact context:\n%s", executor.request.Prompt)
	}
	if strings.Contains(executor.request.Prompt, proposed.Hash) || strings.Contains(executor.request.Prompt, "unapproved proposal") {
		t.Fatalf("prompt included unapproved artifact content: %s", executor.request.Prompt)
	}
	var promptPath string
	if err := db.SQL().QueryRowContext(ctx, "SELECT path FROM run_artifacts WHERE run_id = ? AND artifact_key = 'prompt.md'", result.ImplementationRun).Scan(&promptPath); err != nil {
		t.Fatal(err)
	}
	rawPrompt, err := os.ReadFile(filepath.Join(db.dataRoot, promptPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawPrompt), "approval_notes: "+notes) {
		t.Fatalf("prompt artifact did not preserve approval notes:\n%s", string(rawPrompt))
	}
}

func TestCodexPromptStdinUsesPromptContentNotPath(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	setRealCodexDoctorDetectedForTest(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	executor := &captureCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	}

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", executor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(executor.request.Prompt, "Task ID: TASK-001") {
		t.Fatalf("executor did not receive prompt content: %q", executor.request.Prompt)
	}
	argv := codexExecArgv(projectRoot, "<final-message-path>", "<output-schema-path>", platform.DefaultNetworkPolicy(), "workspace-write")
	if len(argv) == 0 || argv[len(argv)-1] != "-" {
		t.Fatalf("codex argv should read prompt from stdin: %#v", argv)
	}
	joinedArgv := strings.Join(argv, "\x00")
	if strings.Contains(joinedArgv, "--ask-for-approval") {
		t.Fatalf("codex argv uses removed approval flag: %#v", argv)
	}
	if !strings.Contains(joinedArgv, `approval_policy="never"`) || !strings.Contains(joinedArgv, "sandbox_workspace_write.network_access=true") || !strings.Contains(joinedArgv, "sandbox_workspace_write.writable_roots=") {
		t.Fatalf("codex argv missing non-interactive config overrides: %#v", argv)
	}
	for _, arg := range argv {
		if strings.Contains(arg, "Task ID: TASK-001") || strings.HasSuffix(arg, "prompt.md") {
			t.Fatalf("codex argv leaked prompt content or path: %#v", argv)
		}
	}
	var argvJSON string
	if err := db.SQL().QueryRowContext(ctx, "SELECT argv_json FROM command_events WHERE run_id = ? AND command_kind = 'codex'", result.ImplementationRun).Scan(&argvJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(argvJSON, "Task ID: TASK-001") || strings.Contains(argvJSON, "prompt.md") || !strings.Contains(argvJSON, `"-"`) {
		t.Fatalf("command event argv_json = %s", argvJSON)
	}
}

func TestPreviewRealCodexTaskDoesNotMutateTask(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	if runtime.GOOS == "windows" {
		insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
			ID:             "windows-main",
			OSFamily:       platform.OSFamilyWindows,
			Role:           platform.RolePrimary,
			Shell:          platform.ShellPowerShell,
			ProjectRoot:    projectRoot,
			GitProvider:    platform.GitProviderWindows,
			CodexAdapter:   platform.CodexAdapterWindows,
			SandboxProfile: platform.SandboxWindowsNative,
			Status:         "configured",
		})
	} else {
		insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	}
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	result, err := db.PreviewRealCodexTask(ctx, "PROJECT-001", "TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Classification != "ready" || !result.NetworkAccess || result.ApprovalPolicy != "never" {
		t.Fatalf("preview = %#v", result)
	}
	if len(result.Argv) == 0 || !containsString(result.Argv, "--ephemeral") || !containsString(result.Argv, "--ignore-user-config") {
		t.Fatalf("argv = %#v", result.Argv)
	}
	var taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "ready" {
		t.Fatalf("task status = %s", taskStatus)
	}
	var runCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE task_id = 'TASK-001'").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("run count = %d", runCount)
	}
}

func TestRunRealCodexTaskThenVerificationReachesReview(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-sidecar"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	runResult, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runResult.TaskStatus != "verifying" {
		t.Fatalf("run result = %#v", runResult)
	}
	verifyResult, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if verifyResult.TaskStatus != "ready_for_human_review" {
		t.Fatalf("verify result = %#v", verifyResult)
	}
}

func TestVerificationRunsAgainstRealCodexTaskWorktree(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := initStorageGitRepo(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	runResult, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
UPDATE tasks
SET verification_commands_json = ?
WHERE project_id = 'PROJECT-001' AND id = 'TASK-001'`, `[{"id":"codex-output-check","environment":"primary","runner":"auto","required_for_merge":true,"working_dir":"task_worktree","command":{"argv":["sh","-c","test -f codex-output.txt"]},"network":false}]`); err != nil {
		t.Fatal(err)
	}

	verifyResult, err := db.VerifyTask(ctx, "PROJECT-001", "TASK-001", VerifyTaskInput{Adapter: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if verifyResult.TaskStatus != "ready_for_human_review" {
		t.Fatalf("verify result = %#v", verifyResult)
	}
	if verifyResult.Commands[0].WorkingDir != runResult.WorktreeRoot {
		t.Fatalf("verification working dir = %s, want %s", verifyResult.Commands[0].WorkingDir, runResult.WorktreeRoot)
	}
	var evidenceRaw string
	if err := db.SQL().QueryRowContext(ctx, "SELECT evidence_json FROM verification_results WHERE run_id = ?", verifyResult.VerificationRun).Scan(&evidenceRaw); err != nil {
		t.Fatal(err)
	}
	escapedWorktreeRoot := strings.ReplaceAll(runResult.WorktreeRoot, `\`, `\\`)
	if !strings.Contains(evidenceRaw, escapedWorktreeRoot) || !strings.Contains(evidenceRaw, runResult.HeadCommit) || !strings.Contains(evidenceRaw, "verification_plan_hash") {
		t.Fatalf("verification evidence = %s", evidenceRaw)
	}
}

func TestRunRealCodexTaskFailureOpensDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-main"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stderr: "network access is required", ExitCode: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "network_required" {
		t.Fatalf("result = %#v", result)
	}
	var decisionCount, inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM decisions WHERE project_id = 'PROJECT-001' AND task_id = 'TASK-001' AND status = 'open'").Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_type = 'decision' AND status = 'open'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if decisionCount != 1 || inboxCount != 1 {
		t.Fatalf("decision=%d inbox=%d", decisionCount, inboxCount)
	}
}

func TestRunRealCodexTaskInvalidFinalMessageOpensDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-sidecar"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: "not json", ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "schema_validation_failed" {
		t.Fatalf("result = %#v", result)
	}
	var runStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = ?", result.ImplementationRun).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "blocked" {
		t.Fatalf("run status = %s", runStatus)
	}
}

func TestRunRealCodexTaskFinalMessageClassifiesRunStatus(t *testing.T) {
	cases := []struct {
		name               string
		final              string
		wantTaskStatus     string
		wantRunStatus      string
		wantClassification string
	}{
		{
			name:               "blocked",
			final:              `{"status":"blocked","summary":"needs input","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":["needs product decision"]}`,
			wantTaskStatus:     "needs_decision",
			wantRunStatus:      "blocked",
			wantClassification: "blocked",
		},
		{
			name:               "failed",
			final:              `{"status":"failed","summary":"implementation failed","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`,
			wantTaskStatus:     "failed",
			wantRunStatus:      "failed",
			wantClassification: "codex_failed",
		},
		{
			name:               "tests failed",
			final:              `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"failed","notes":"compile error"}],"blockers":[]}`,
			wantTaskStatus:     "failed",
			wantRunStatus:      "failed",
			wantClassification: "tests_failed",
		},
		{
			name:               "tests not run",
			final:              `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"not_run","notes":"not available"}],"blockers":[]}`,
			wantTaskStatus:     "needs_decision",
			wantRunStatus:      "blocked",
			wantClassification: "tests_not_run",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openMigratedTestDB(t)
			ctx := context.Background()
			projectRoot := initStorageGitRepo(t)
			insertProject(t, db.SQL(), "PROJECT-001")
			insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
			insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
			setRealCodexDoctorDetectedForTest(t)

			result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
				result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: tc.final, ExitCode: 0},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.TaskStatus != tc.wantTaskStatus || result.Classification != tc.wantClassification {
				t.Fatalf("result = %#v", result)
			}
			var runStatus string
			if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = ?", result.ImplementationRun).Scan(&runStatus); err != nil {
				t.Fatal(err)
			}
			if runStatus != tc.wantRunStatus {
				t.Fatalf("run status = %s", runStatus)
			}
		})
	}
}

func TestRunRealCodexTaskDependencyCommandBlocksAndSavesNetworkEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := initStorageGitRepo(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)
	stdout := `{"type":"item.completed","item":{"type":"command_execution","command":"npm install zod"}}` + "\n"

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: stdout, FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "policy_blocked" {
		t.Fatalf("result = %#v", result)
	}
	var artifactPath string
	if err := db.SQL().QueryRowContext(ctx, "SELECT path FROM run_artifacts WHERE run_id = ? AND artifact_key = 'network-evidence.json'", result.ImplementationRun).Scan(&artifactPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(db.dataRoot, artifactPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "dependency installation requires approval") {
		t.Fatalf("network evidence = %s", string(raw))
	}
}

func TestRunRealCodexTaskSupportsWSLPrimaryEnvironment(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-main"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "wsl-main",
		OSFamily:       platform.OSFamilyWSL,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	})
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)
	restore := setRealCodexRuntimeGOOSForTest("linux")
	defer restore()

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "verifying" || result.Classification != "succeeded" {
		t.Fatalf("result = %#v", result)
	}
	var environmentID, runner string
	if err := db.SQL().QueryRowContext(ctx, "SELECT environment_id, runner FROM command_events WHERE run_id = ?", result.ImplementationRun).Scan(&environmentID, &runner); err != nil {
		t.Fatal(err)
	}
	if environmentID != "wsl-main" || runner != "direct" {
		t.Fatalf("command event environment=%s runner=%s", environmentID, runner)
	}
}

func TestRunRealCodexTaskUsesRunProfileImplementationEnvironment(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "wsl-sidecar",
		OSFamily:       platform.OSFamilyWSL,
		Role:           platform.RoleSidecar,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	})
	insertActiveRunProfile(t, db, "PROJECT-001", "linux-main", "wsl-sidecar")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)
	restore := setRealCodexRuntimeGOOSForTest("linux")
	defer restore()

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	var environmentID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT implementation_environment_id FROM runs WHERE id = ?", result.ImplementationRun).Scan(&environmentID); err != nil {
		t.Fatal(err)
	}
	if environmentID != "wsl-sidecar" {
		t.Fatalf("implementation environment = %s", environmentID)
	}
}

func TestRunRealCodexTaskSupportsWindowsWhenRuntimeIsWindows(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "windows-main",
		OSFamily:       platform.OSFamilyWindows,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellPowerShell,
		ProjectRoot:    `C:\dev\project`,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
		Status:         "configured",
	})
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)
	restore := setRealCodexRuntimeGOOSForTest("windows")
	defer restore()

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`, ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "verifying" || result.Classification != "succeeded" {
		t.Fatalf("result = %#v", result)
	}
	var environmentID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT implementation_environment_id FROM runs WHERE id = ?", result.ImplementationRun).Scan(&environmentID); err != nil {
		t.Fatal(err)
	}
	if environmentID != "windows-main" {
		t.Fatalf("implementation environment = %s", environmentID)
	}
}

func TestWindowsNativeCodexUsesDangerFullAccessSandbox(t *testing.T) {
	env := platform.ExecutionEnvironment{
		OSFamily:       platform.OSFamilyWindows,
		SandboxProfile: platform.SandboxWindowsNative,
	}
	if got := codexSandboxMode(env); got != "danger-full-access" {
		t.Fatalf("sandbox mode = %s", got)
	}
	argv := codexExecArgv(`C:\dev\project`, "<final>", "<schema>", platform.DefaultNetworkPolicy(), codexSandboxMode(env))
	if !containsString(argv, "danger-full-access") || containsString(argv, "--add-dir") {
		t.Fatalf("windows argv = %#v", argv)
	}
}

func TestRunRealCodexTaskBlocksWindowsWhenRuntimeIsNotWindows(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "windows-main",
		OSFamily:       platform.OSFamilyWindows,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellPowerShell,
		ProjectRoot:    `C:\dev\project`,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
		Status:         "configured",
	})
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	restore := setRealCodexRuntimeGOOSForTest("linux")
	defer restore()

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"should-not-run\"}\n", FinalMessage: "should not run", ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "windows_codex_adapter_requires_windows_runtime" {
		t.Fatalf("result = %#v", result)
	}
	var runStatus, taskStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE id = ?", result.ImplementationRun).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = 'TASK-001'").Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "blocked" || taskStatus != "needs_decision" {
		t.Fatalf("run=%s task=%s", runStatus, taskStatus)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_type = 'decision' AND status = 'open'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count = %d", inboxCount)
	}
}

func TestCodexRuntimeReadinessReportsPerEnvironmentCompatibility(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-sidecar"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "windows-main",
		OSFamily:       platform.OSFamilyWindows,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellPowerShell,
		ProjectRoot:    `C:\dev\project`,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
		Status:         "configured",
	})
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "wsl-sidecar",
		OSFamily:       platform.OSFamilyWSL,
		Role:           platform.RoleSidecar,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	})
	restore := setRealCodexRuntimeGOOSForTest("linux")
	defer restore()
	setRealCodexDoctorDetectedForTest(t)

	report, err := db.CodexRuntimeReadiness(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if report.HostGOOS != "linux" || len(report.Items) != 2 {
		t.Fatalf("report = %#v", report)
	}
	byID := map[string]CodexRuntimeReadinessItem{}
	for _, item := range report.Items {
		byID[item.EnvironmentID] = item
	}
	if byID["windows-main"].CurrentRuntimeUsable || byID["windows-main"].Classification != "windows_codex_adapter_requires_windows_runtime" {
		t.Fatalf("windows readiness = %#v", byID["windows-main"])
	}
	if !byID["wsl-sidecar"].CurrentRuntimeUsable || byID["wsl-sidecar"].ExpectedHostRuntime != "linux" || len(byID["wsl-sidecar"].Argv) == 0 {
		t.Fatalf("wsl readiness = %#v", byID["wsl-sidecar"])
	}
}

func TestCodexRuntimeReadinessReportsToolchainBlockers(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := "/tmp/devos-wsl-main"
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "wsl-main",
		OSFamily:       platform.OSFamilyWSL,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	})
	restoreRuntime := setRealCodexRuntimeGOOSForTest("linux")
	defer restoreRuntime()
	restoreDoctor := setRealCodexDoctorForTest(func(ctx context.Context, env platform.ExecutionEnvironment, opts toolchains.Options) toolchains.Report {
		_ = ctx
		_ = env
		_ = opts
		return toolchains.Report{
			EnvironmentID: "wsl-main",
			Requirements: []toolchains.Requirement{
				{
					ToolchainKey:     "codex-auth",
					RequiredFor:      toolchains.RequiredForImplementation,
					RequiredForMerge: true,
					Status:           toolchains.StatusSetupRequired,
					Message:          "Codex auth is not detected",
				},
			},
		}
	})
	defer restoreDoctor()

	report, err := db.CodexRuntimeReadiness(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 1 {
		t.Fatalf("report = %#v", report)
	}
	item := report.Items[0]
	if item.CurrentRuntimeUsable || item.Classification != "toolchain_required" || len(item.Blockers) != 1 || len(item.Argv) != 0 {
		t.Fatalf("readiness item = %#v", item)
	}
}

func TestSaveCodexRuntimeReadinessProjectsInboxIssues(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertCodexEnvironment(t, db, "PROJECT-001", platform.ExecutionEnvironment{
		ID:             "windows-main",
		OSFamily:       platform.OSFamilyWindows,
		Role:           platform.RolePrimary,
		Shell:          platform.ShellPowerShell,
		ProjectRoot:    `C:\dev\project`,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
		Status:         "configured",
	})
	restore := setRealCodexRuntimeGOOSForTest("linux")
	setRealCodexDoctorDetectedForTest(t)
	report, err := db.CodexRuntimeReadiness(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	items, err := db.SaveCodexRuntimeReadiness(ctx, "PROJECT-001", report)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ItemType != "runner_capability_issue" || items[0].SourceID != "windows-main" {
		t.Fatalf("inbox items = %#v", items)
	}
	restore()
	restore = setRealCodexRuntimeGOOSForTest("windows")
	defer restore()
	report, err = db.CodexRuntimeReadiness(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	items, err = db.SaveCodexRuntimeReadiness(ctx, "PROJECT-001", report)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected resolved runtime issues, got %#v", items)
	}
}

func TestRunRealCodexTaskBlocksMissingRequiredToolchain(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	restoreRuntime := setRealCodexRuntimeGOOSForTest("linux")
	defer restoreRuntime()
	restoreDoctor := setRealCodexDoctorForTest(func(ctx context.Context, env platform.ExecutionEnvironment, opts toolchains.Options) toolchains.Report {
		_ = ctx
		_ = env
		_ = opts
		return toolchains.Report{
			EnvironmentID: "linux-main",
			Requirements: []toolchains.Requirement{
				{
					ToolchainKey:     "codex-auth",
					RequiredFor:      toolchains.RequiredForImplementation,
					RequiredForMerge: true,
					Status:           toolchains.StatusSetupRequired,
					Message:          "Codex auth is not detected",
				},
			},
		}
	})
	defer restoreDoctor()

	result, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"should-not-run\"}\n", FinalMessage: "should not run", ExitCode: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskStatus != "needs_decision" || result.Classification != "toolchain_required" {
		t.Fatalf("result = %#v", result)
	}
	var setupCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'toolchain_setup' AND status = 'open'").Scan(&setupCount); err != nil {
		t.Fatal(err)
	}
	if setupCount != 1 {
		t.Fatalf("setup inbox count = %d", setupCount)
	}
}

func setRealCodexRuntimeGOOSForTest(goos string) func() {
	original := realCodexRuntimeGOOS
	realCodexRuntimeGOOS = goos
	return func() {
		realCodexRuntimeGOOS = original
	}
}

func setRealCodexDoctorDetectedForTest(t *testing.T) {
	t.Helper()
	restore := setRealCodexDoctorForTest(func(ctx context.Context, env platform.ExecutionEnvironment, opts toolchains.Options) toolchains.Report {
		_ = ctx
		_ = opts
		shellKey := string(env.Shell)
		if shellKey == "" {
			shellKey = "bash"
		}
		return toolchains.Report{
			EnvironmentID: env.ID,
			Requirements: []toolchains.Requirement{
				{ToolchainKey: "git", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected, Message: "git detected"},
				{ToolchainKey: shellKey, RequiredFor: toolchains.RequiredForVerification, RequiredForMerge: true, Status: toolchains.StatusDetected, Message: shellKey + " detected"},
				{ToolchainKey: "codex", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected, Message: "codex detected"},
				{ToolchainKey: "codex-auth", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected, Message: "codex auth detected"},
			},
		}
	})
	t.Cleanup(restore)
}

func setRealCodexDoctorForTest(fn func(context.Context, platform.ExecutionEnvironment, toolchains.Options) toolchains.Report) func() {
	original := runRealCodexDoctor
	runRealCodexDoctor = fn
	return func() {
		runRealCodexDoctor = original
	}
}

func insertCodexEnvironment(t *testing.T, db *DB, projectID string, env platform.ExecutionEnvironment) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.ID, projectID, env.OSFamily, env.Role, env.Shell, env.ProjectRoot, env.GitProvider,
		env.CodexAdapter, env.SandboxProfile, env.Status, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

func insertActiveRunProfile(t *testing.T, db *DB, projectID string, primaryEnvID string, implementationEnvID string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO project_run_profiles(
  id, project_id, name, mode, status, primary_environment_id, implementation_environment_id,
  git_environment_id, merge_environment_id, required_verification_environment_ids_json,
  optional_verification_environment_ids_json, canonical_operations_json, created_at, updated_at
) VALUES (
  'RUNPROFILE-TEST', ?, 'test-profile', 'hybrid', 'active', ?, ?, ?, ?, '[]', '[]', '{}', ?, ?
)`,
		projectID, primaryEnvID, implementationEnvID, primaryEnvID, primaryEnvID, now(), now())
	if err != nil {
		t.Fatal(err)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
