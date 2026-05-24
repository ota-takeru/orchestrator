package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done"}`, ExitCode: 0},
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

func TestRunRealCodexTaskThenVerificationReachesReview(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", projectRoot)
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")
	setRealCodexDoctorDetectedForTest(t)

	runResult, err := db.RunRealCodexTask(ctx, "PROJECT-001", "TASK-001", fakeCodexExecutor{
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done"}`, ExitCode: 0},
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

func TestRunRealCodexTaskFailureOpensDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
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
	projectRoot := t.TempDir()
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

func TestRunRealCodexTaskSupportsWSLPrimaryEnvironment(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()
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
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done"}`, ExitCode: 0},
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
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done"}`, ExitCode: 0},
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
		result: CodexExecResult{Stdout: "{\"type\":\"done\"}\n", FinalMessage: `{"status":"succeeded","summary":"done"}`, ExitCode: 0},
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
				{ToolchainKey: "git", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected},
				{ToolchainKey: shellKey, RequiredFor: toolchains.RequiredForVerification, RequiredForMerge: true, Status: toolchains.StatusDetected},
				{ToolchainKey: "codex", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected},
				{ToolchainKey: "codex-auth", RequiredFor: toolchains.RequiredForImplementation, RequiredForMerge: true, Status: toolchains.StatusDetected},
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
