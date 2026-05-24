package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/schemas"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type CodexExecRequest struct {
	ProjectRoot           string
	Prompt                string
	OutputLastMessagePath string
	OutputSchemaPath      string
}

type CodexExecResult struct {
	Stdout       string
	Stderr       string
	FinalMessage string
	ExitCode     int
	StartedAt    time.Time
	CompletedAt  time.Time
}

type CodexExecutor interface {
	ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error)
}

type LocalCodexExecutor struct{}

type RealCodexRunResult struct {
	TaskID            string   `json:"task_id"`
	TaskStatus        string   `json:"task_status"`
	ImplementationRun string   `json:"implementation_run_id"`
	Classification    string   `json:"classification"`
	Blockers          []string `json:"blockers,omitempty"`
}

type RealCodexPreviewResult struct {
	TaskID         string   `json:"task_id"`
	TaskStatus     string   `json:"task_status"`
	EnvironmentID  string   `json:"environment_id"`
	ProjectRoot    string   `json:"project_root"`
	CodexAdapter   string   `json:"codex_adapter"`
	SandboxProfile string   `json:"sandbox_profile"`
	NetworkAccess  bool     `json:"network_access"`
	ApprovalPolicy string   `json:"approval_policy"`
	Classification string   `json:"classification"`
	Blockers       []string `json:"blockers,omitempty"`
	Argv           []string `json:"argv"`
}

type CodexRuntimeReadinessReport struct {
	HostGOOS string                      `json:"host_goos"`
	Items    []CodexRuntimeReadinessItem `json:"items"`
}

type CodexRuntimeReadinessItem struct {
	EnvironmentID        string   `json:"environment_id"`
	OSFamily             string   `json:"os_family"`
	ProjectRoot          string   `json:"project_root"`
	CodexAdapter         string   `json:"codex_adapter"`
	SandboxProfile       string   `json:"sandbox_profile"`
	ExpectedHostRuntime  string   `json:"expected_host_runtime"`
	CurrentRuntimeUsable bool     `json:"current_runtime_usable"`
	Classification       string   `json:"classification"`
	Blockers             []string `json:"blockers,omitempty"`
	Argv                 []string `json:"argv,omitempty"`
}

var realCodexRuntimeGOOS = runtime.GOOS

var runRealCodexDoctor = toolchains.RunDoctor

func (LocalCodexExecutor) ExecCodex(ctx context.Context, request CodexExecRequest) (CodexExecResult, error) {
	finalFile, err := os.CreateTemp("", "devos-codex-final-*.txt")
	if err != nil {
		return CodexExecResult{}, err
	}
	finalPath := finalFile.Name()
	_ = finalFile.Close()
	defer os.Remove(finalPath)
	if request.OutputLastMessagePath != "" {
		finalPath = request.OutputLastMessagePath
	}
	schemaFile, err := os.CreateTemp("", "devos-codex-output-schema-*.json")
	if err != nil {
		return CodexExecResult{}, err
	}
	schemaPath := schemaFile.Name()
	if _, err := schemaFile.Write(schemas.CodexFinalMessageSchema()); err != nil {
		_ = schemaFile.Close()
		return CodexExecResult{}, err
	}
	if err := schemaFile.Close(); err != nil {
		return CodexExecResult{}, err
	}
	defer os.Remove(schemaPath)
	if request.OutputSchemaPath != "" {
		schemaPath = request.OutputSchemaPath
	}

	args := codexExecArgv(request.ProjectRoot, request.Prompt, finalPath, schemaPath)
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = request.ProjectRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now().UTC()
	err = cmd.Run()
	completed := time.Now().UTC()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return CodexExecResult{}, err
		}
	}
	finalMessage, _ := os.ReadFile(finalPath)
	return CodexExecResult{
		Stdout:       stdout.String(),
		Stderr:       stderr.String(),
		FinalMessage: string(finalMessage),
		ExitCode:     exitCode,
		StartedAt:    started,
		CompletedAt:  completed,
	}, nil
}

func codexExecArgv(projectRoot string, prompt string, finalPath string, schemaPath string) []string {
	return []string{
		"exec",
		"--json",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--sandbox", "workspace-write",
		"--ask-for-approval", "never",
		"-c", "sandbox_workspace_write.network_access=false",
		"--cd", projectRoot,
		"-o", finalPath,
		"--output-schema", schemaPath,
		prompt,
	}
}

func (db *DB) RunRealCodexTask(ctx context.Context, projectID string, taskID string, executor CodexExecutor) (RealCodexRunResult, error) {
	if executor == nil {
		executor = LocalCodexExecutor{}
	}
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return RealCodexRunResult{}, err
	}
	if status != "ready" {
		return RealCodexRunResult{}, fmt.Errorf("task %s is not ready: %s", taskID, status)
	}
	env, err := db.ResolveImplementationEnvironment(ctx, projectID)
	if err != nil {
		return RealCodexRunResult{}, err
	}
	classification, blockers := evaluateRealCodexEnvironment(env, realCodexRuntimeGOOS)
	if len(blockers) > 0 {
		return db.recordRealCodexAdapterBlocked(ctx, projectID, taskID, env, classification, blockers)
	}
	doctorReport := runRealCodexDoctor(ctx, env, toolchains.Options{IncludeCodex: true})
	if err := db.SaveToolchainReport(ctx, projectID, doctorReport); err != nil {
		return RealCodexRunResult{}, err
	}
	if blockers := realCodexToolchainBlockers(doctorReport); len(blockers) > 0 {
		return db.recordRealCodexAdapterBlocked(ctx, projectID, taskID, env, "toolchain_required", blockers)
	}
	if err := db.transitionTask(ctx, projectID, taskID, "ready", "implementing", "real_codex_implementation_started", map[string]any{"task_id": taskID, "environment_id": env.ID}); err != nil {
		return RealCodexRunResult{}, err
	}

	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "implementation")
	if err != nil {
		return RealCodexRunResult{}, err
	}
	baseCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	prompt := realCodexPrompt(taskID)
	execResult, err := executor.ExecCodex(ctx, CodexExecRequest{ProjectRoot: env.ProjectRoot, Prompt: prompt})
	if err != nil {
		return RealCodexRunResult{}, err
	}
	if execResult.ExitCode == 0 {
		if err := schemas.ValidateCodexFinalMessage(execResult.FinalMessage); err != nil {
			execResult.ExitCode = 1
			execResult.Stderr = strings.TrimSpace(execResult.Stderr + "\n" + "schema validation failed: " + err.Error())
		}
	}
	headCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	diff := gitOutputOrEmpty(ctx, env.ProjectRoot, "diff", "--binary")
	diffHash := sha256Hex([]byte(diff))
	classification, blockers = classifyCodexExecResult(execResult)
	runStatus := "succeeded"
	taskTo := "verifying"
	if len(blockers) > 0 {
		runStatus = "blocked"
		taskTo = "needs_decision"
	}
	runID := "RUN-" + stableShortHash(taskID+"|real-codex|"+time.Now().UTC().Format(time.RFC3339Nano))
	if err := db.saveCodexRun(ctx, projectID, taskID, env, runID, attemptNo, baseCommit, headCommit, diffHash, diff, prompt, execResult, runStatus, classification, blockers); err != nil {
		return RealCodexRunResult{}, err
	}
	if taskTo == "needs_decision" {
		if err := db.openCodexBlockedDecision(ctx, projectID, taskID, runID, classification, blockers); err != nil {
			return RealCodexRunResult{}, err
		}
	}
	if err := db.transitionTask(ctx, projectID, taskID, "implementing", taskTo, "real_codex_implementation_completed", map[string]any{
		"task_id":  taskID,
		"run_id":   runID,
		"status":   runStatus,
		"blockers": blockers,
	}); err != nil {
		return RealCodexRunResult{}, err
	}
	return RealCodexRunResult{TaskID: taskID, TaskStatus: taskTo, ImplementationRun: runID, Classification: classification, Blockers: blockers}, nil
}

func (db *DB) PreviewRealCodexTask(ctx context.Context, projectID string, taskID string) (RealCodexPreviewResult, error) {
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return RealCodexPreviewResult{}, err
	}
	env, err := db.ResolveImplementationEnvironment(ctx, projectID)
	if err != nil {
		return RealCodexPreviewResult{}, err
	}
	classification, blockers := evaluateRealCodexEnvironment(env, realCodexRuntimeGOOS)
	if status != "ready" {
		classification = "task_not_ready"
		blockers = append(blockers, "task_not_ready:"+status)
	}
	if len(blockers) == 0 {
		doctorReport := runRealCodexDoctor(ctx, env, toolchains.Options{IncludeCodex: true})
		blockers = realCodexToolchainBlockers(doctorReport)
		if len(blockers) > 0 {
			classification = "toolchain_required"
		}
	}
	return RealCodexPreviewResult{
		TaskID:         taskID,
		TaskStatus:     status,
		EnvironmentID:  env.ID,
		ProjectRoot:    env.ProjectRoot,
		CodexAdapter:   string(env.CodexAdapter),
		SandboxProfile: string(env.SandboxProfile),
		NetworkAccess:  false,
		ApprovalPolicy: "never",
		Classification: classification,
		Blockers:       blockers,
		Argv:           codexExecArgv(env.ProjectRoot, "<prompt>", "<final-message-path>", "<output-schema-path>"),
	}, nil
}

func (db *DB) CodexRuntimeReadiness(ctx context.Context, projectID string) (CodexRuntimeReadinessReport, error) {
	envs, err := db.ListExecutionEnvironments(ctx, projectID)
	if err != nil {
		return CodexRuntimeReadinessReport{}, err
	}
	report := CodexRuntimeReadinessReport{HostGOOS: realCodexRuntimeGOOS}
	for _, env := range envs {
		classification, blockers := evaluateRealCodexEnvironment(env, realCodexRuntimeGOOS)
		if len(blockers) == 0 && env.CodexAdapter != platform.CodexAdapterNone {
			doctorReport := runRealCodexDoctor(ctx, env, toolchains.Options{IncludeCodex: true})
			if toolchainBlockers := realCodexToolchainBlockers(doctorReport); len(toolchainBlockers) > 0 {
				classification = "toolchain_required"
				blockers = toolchainBlockers
			}
		}
		item := CodexRuntimeReadinessItem{
			EnvironmentID:        env.ID,
			OSFamily:             string(env.OSFamily),
			ProjectRoot:          env.ProjectRoot,
			CodexAdapter:         string(env.CodexAdapter),
			SandboxProfile:       string(env.SandboxProfile),
			ExpectedHostRuntime:  expectedCodexHostRuntime(env),
			CurrentRuntimeUsable: len(blockers) == 0,
			Classification:       classification,
			Blockers:             blockers,
		}
		if item.CurrentRuntimeUsable {
			item.Argv = codexExecArgv(env.ProjectRoot, "<prompt>", "<final-message-path>", "<output-schema-path>")
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func (db *DB) SaveCodexRuntimeReadiness(ctx context.Context, projectID string, report CodexRuntimeReadinessReport) ([]InboxItem, error) {
	payload, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if err := schemas.ValidateCodexRuntimeReadiness(string(payload)); err != nil {
		return nil, err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range report.Items {
		dedupeKey := strings.Join([]string{projectID, "codex_runtime_readiness", item.EnvironmentID}, ":")
		if item.CurrentRuntimeUsable {
			if _, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND source_type = 'execution_environment' AND source_id = ? AND dedupe_key = ? AND status = 'open'`,
				now, now, projectID, item.EnvironmentID, dedupeKey,
			); err != nil {
				return nil, err
			}
			continue
		}
		body := fmt.Sprintf("Classification: %s\nExpected runtime: %s\nBlockers: %s", item.Classification, item.ExpectedHostRuntime, strings.Join(item.Blockers, ", "))
		inboxID := "INBOX-" + stableShortHash(dedupeKey)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, 'runner_capability_issue', 'open', 'execution_environment', ?, ?, ?, 75, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  updated_at = excluded.updated_at`,
			inboxID, projectID, item.EnvironmentID, dedupeKey, projectID+":codex_runtime_readiness",
			"Codex runtime not usable: "+item.EnvironmentID, body, now, now,
		); err != nil {
			return nil, err
		}
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "codex_runtime_readiness_saved", map[string]any{
		"host_goos": report.HostGOOS,
		"items":     len(report.Items),
	}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	items, err := db.ListInboxItems(ctx, projectID, "open")
	if err != nil {
		return nil, err
	}
	filtered := make([]InboxItem, 0, len(items))
	for _, item := range items {
		if item.SourceType == "execution_environment" && strings.HasPrefix(item.Title, "Codex runtime not usable: ") {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func evaluateRealCodexEnvironment(env platform.ExecutionEnvironment, hostGOOS string) (string, []string) {
	if strings.TrimSpace(env.ProjectRoot) == "" {
		return "project_root_missing", []string{"project_root_missing"}
	}
	switch env.OSFamily {
	case platform.OSFamilyLinux:
		if env.CodexAdapter != platform.CodexAdapterLinux {
			return "codex_adapter_mismatch", []string{"linux_environment_requires_codex_linux"}
		}
		if hostGOOS != "linux" {
			return "linux_codex_adapter_requires_linux_runtime", []string{"linux_codex_adapter_requires_linux_runtime"}
		}
		if !isPOSIXShell(env.Shell) {
			return "shell_mismatch", []string{"linux_codex_adapter_requires_posix_shell"}
		}
		if !isPOSIXRoot(env.ProjectRoot) {
			return "project_root_mismatch", []string{"linux_codex_adapter_requires_posix_project_root"}
		}
		return "ready", nil
	case platform.OSFamilyWSL:
		if env.CodexAdapter != platform.CodexAdapterWSL {
			return "codex_adapter_mismatch", []string{"wsl_environment_requires_codex_wsl"}
		}
		if hostGOOS != "linux" {
			return "wsl_codex_adapter_requires_linux_runtime", []string{"wsl_codex_adapter_requires_linux_runtime"}
		}
		if !isPOSIXShell(env.Shell) {
			return "shell_mismatch", []string{"wsl_codex_adapter_requires_posix_shell"}
		}
		if !isPOSIXRoot(env.ProjectRoot) {
			return "project_root_mismatch", []string{"wsl_codex_adapter_requires_posix_project_root"}
		}
		return "ready", nil
	case platform.OSFamilyWindows:
		if env.CodexAdapter != platform.CodexAdapterWindows {
			return "codex_adapter_mismatch", []string{"windows_environment_requires_codex_windows"}
		}
		if hostGOOS != "windows" {
			return "windows_codex_adapter_requires_windows_runtime", []string{"windows_codex_adapter_requires_windows_runtime"}
		}
		if env.Shell != platform.ShellPowerShell && env.Shell != platform.ShellCmd {
			return "shell_mismatch", []string{"windows_codex_adapter_requires_windows_shell"}
		}
		if !isWindowsRoot(env.ProjectRoot) {
			return "project_root_mismatch", []string{"windows_codex_adapter_requires_windows_project_root"}
		}
		return "ready", nil
	case platform.OSFamilyRemoteWindows, platform.OSFamilyRemoteLinux:
		return "remote_runner_required", []string{"real_codex_remote_runner_not_configured"}
	default:
		return "unsupported_os_family", []string{"real_codex_adapter_unsupported_os_family"}
	}
}

func expectedCodexHostRuntime(env platform.ExecutionEnvironment) string {
	switch env.OSFamily {
	case platform.OSFamilyWindows:
		return "windows"
	case platform.OSFamilyLinux, platform.OSFamilyWSL:
		return "linux"
	case platform.OSFamilyRemoteWindows:
		return "remote_windows_runner"
	case platform.OSFamilyRemoteLinux:
		return "remote_linux_runner"
	default:
		return "unsupported"
	}
}

func realCodexToolchainBlockers(report toolchains.Report) []string {
	var blockers []string
	for _, req := range report.Requirements {
		if !req.RequiredForMerge {
			continue
		}
		switch req.Status {
		case toolchains.StatusMissing, toolchains.StatusInvalid, toolchains.StatusSetupRequired, toolchains.StatusUnsupported:
			blockers = append(blockers, string(req.ToolchainKey)+":"+string(req.Status))
		}
	}
	return blockers
}

func isPOSIXShell(shell platform.Shell) bool {
	return shell == platform.ShellBash || shell == platform.ShellSh
}

func isPOSIXRoot(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "/")
}

func isWindowsRoot(path string) bool {
	trimmed := strings.TrimSpace(path)
	if len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/') {
		return true
	}
	return strings.HasPrefix(trimmed, `\\`)
}

func (db *DB) recordRealCodexAdapterBlocked(ctx context.Context, projectID string, taskID string, env platform.ExecutionEnvironment, classification string, blockers []string) (RealCodexRunResult, error) {
	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "implementation")
	if err != nil {
		return RealCodexRunResult{}, err
	}
	now := time.Now().UTC()
	runID := "RUN-" + stableShortHash(taskID+"|real-codex-adapter-blocked|"+now.Format(time.RFC3339Nano))
	baseCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	headCommit := baseCommit
	prompt := realCodexPrompt(taskID)
	blockerText := strings.Join(blockers, ", ")
	execResult := CodexExecResult{
		Stdout:       "",
		Stderr:       "real Codex adapter blocked before process start: " + blockerText,
		FinalMessage: "real Codex adapter blocked before process start: " + blockerText,
		ExitCode:     1,
		StartedAt:    now,
		CompletedAt:  now,
	}
	if err := db.saveCodexRun(ctx, projectID, taskID, env, runID, attemptNo, baseCommit, headCommit, sha256Hex(nil), "", prompt, execResult, "blocked", classification, blockers); err != nil {
		return RealCodexRunResult{}, err
	}
	if err := db.openCodexBlockedDecision(ctx, projectID, taskID, runID, classification, blockers); err != nil {
		return RealCodexRunResult{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "ready", "needs_decision", "real_codex_adapter_blocked", map[string]any{
		"task_id":        taskID,
		"run_id":         runID,
		"environment_id": env.ID,
		"classification": classification,
		"blockers":       blockers,
	}); err != nil {
		return RealCodexRunResult{}, err
	}
	return RealCodexRunResult{TaskID: taskID, TaskStatus: "needs_decision", ImplementationRun: runID, Classification: classification, Blockers: blockers}, nil
}

func realCodexPrompt(taskID string) string {
	return strings.Join([]string{
		"Implement the assigned DevOS task in this repository.",
		"Task ID: " + taskID,
		"Follow AGENTS.md and project documentation.",
		"Do not request interactive approvals.",
		"Do not use network access.",
		"Stop if dependency installation, outside-workspace writes, destructive git commands, or permission escalation are required.",
	}, "\n")
}

func classifyCodexExecResult(result CodexExecResult) (string, []string) {
	if result.ExitCode == 0 {
		return "succeeded", nil
	}
	output := strings.ToLower(result.Stdout + "\n" + result.Stderr + "\n" + result.FinalMessage)
	classification := "codex_failed"
	switch {
	case strings.Contains(output, "network") || strings.Contains(output, "dns") || strings.Contains(output, "connection refused"):
		classification = "network_required"
	case strings.Contains(output, "approval") || strings.Contains(output, "permission") || strings.Contains(output, "escalat"):
		classification = "permission_required"
	case strings.Contains(output, "outside workspace") || strings.Contains(output, "sandbox"):
		classification = "sandbox_blocked"
	case strings.Contains(output, "install") || strings.Contains(output, "dependency") || strings.Contains(output, "package"):
		classification = "dependency_required"
	case strings.Contains(output, "git reset") || strings.Contains(output, "git clean") || strings.Contains(output, "destructive"):
		classification = "destructive_command_required"
	case strings.Contains(output, "schema validation"):
		classification = "schema_validation_failed"
	}
	return classification, []string{classification}
}

func (db *DB) saveCodexRun(ctx context.Context, projectID string, taskID string, env platform.ExecutionEnvironment, runID string, attemptNo int, baseCommit string, headCommit string, diffHash string, diff string, prompt string, execResult CodexExecResult, runStatus string, classification string, blockers []string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &taskID,
		RunID:      runID,
		RunType:    "implementation",
		AttemptNo:  attemptNo,
		BaseCommit: baseCommit,
	}, runStatus, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET implementation_environment_id = ?, head_commit = ?, diff_hash = ?, sandbox_profile = ? WHERE project_id = ? AND id = ?", env.ID, headCommit, diffHash, env.SandboxProfile, projectID, runID); err != nil {
		return err
	}
	command := verifier.Command{
		ID:            "codex-exec",
		EnvironmentID: env.ID,
		Runner:        "direct",
		WorkingDir:    env.ProjectRoot,
		Argv:          codexExecArgv(env.ProjectRoot, "<prompt>", "<final-message-path>", "<output-schema-path>"),
		NetworkPolicy: runners.NetworkOff,
	}
	commandStatus := runners.CommandSucceeded
	if execResult.ExitCode != 0 {
		commandStatus = runners.CommandBlocked
	}
	commandResult := runners.RunCommandResult{
		EnvironmentID: env.ID,
		ExitCode:      execResult.ExitCode,
		Status:        commandStatus,
		StartedAt:     execResult.StartedAt,
		CompletedAt:   execResult.CompletedAt,
		Stdout:        execResult.Stdout,
		Stderr:        execResult.Stderr,
		DetectedRisks: blockers,
	}
	commandEventID := commandEventID(runID, command.ID, env.ID)
	stdoutArtifactID, stderrArtifactID, err := db.saveCommandOutputArtifacts(ctx, tx, projectID, runID, commandEventID, command.ID, env.ID, commandResult, now)
	if err != nil {
		return err
	}
	if err := insertCommandEvent(ctx, tx, projectID, runID, commandEventID, "codex", command, commandResult, stdoutArtifactID, stderrArtifactID, now); err != nil {
		return err
	}
	artifacts := []RunArtifactInput{
		{ProjectID: projectID, RunID: runID, ArtifactType: "prompt", ArtifactKey: "prompt.md", Content: []byte(prompt)},
		{ProjectID: projectID, RunID: runID, ArtifactType: "events_jsonl", ArtifactKey: "events.redacted.jsonl", Content: []byte(execResult.Stdout)},
		{ProjectID: projectID, RunID: runID, ArtifactType: "final_message", ArtifactKey: "final.txt", Content: []byte(execResult.FinalMessage)},
		{ProjectID: projectID, RunID: runID, ArtifactType: "diff", ArtifactKey: "diff.patch", Content: []byte(diff)},
	}
	summary, err := json.MarshalIndent(map[string]any{
		"task_id":           taskID,
		"run_id":            runID,
		"status":            runStatus,
		"classification":    classification,
		"blockers":          blockers,
		"exit_code":         execResult.ExitCode,
		"environment_id":    env.ID,
		"codex_adapter":     env.CodexAdapter,
		"sandbox_profile":   env.SandboxProfile,
		"codex_home_source": codexHomeSourceForEnvironment(env, os.LookupEnv),
	}, "", "  ")
	if err != nil {
		return err
	}
	artifacts = append(artifacts, RunArtifactInput{ProjectID: projectID, RunID: runID, ArtifactType: "summary", ArtifactKey: "real-codex-summary.json", Content: summary})
	for _, artifact := range artifacts {
		if _, err := db.saveRunArtifactInTx(ctx, tx, artifact, now); err != nil {
			return err
		}
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "real_codex_run_recorded", map[string]any{
		"task_id":        taskID,
		"run_id":         runID,
		"status":         runStatus,
		"classification": classification,
		"blockers":       blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func codexHomeSourceForEnvironment(env platform.ExecutionEnvironment, lookupEnv func(string) (string, bool)) string {
	if value, ok := lookupEnv("CODEX_HOME"); ok && strings.TrimSpace(value) != "" {
		return "CODEX_HOME"
	}
	switch env.OSFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		if value, ok := lookupEnv("USERPROFILE"); ok && strings.TrimSpace(value) != "" {
			return "USERPROFILE"
		}
		drive, driveOK := lookupEnv("HOMEDRIVE")
		path, pathOK := lookupEnv("HOMEPATH")
		if driveOK && pathOK && strings.TrimSpace(drive+path) != "" {
			return "HOMEDRIVE/HOMEPATH"
		}
	default:
		if value, ok := lookupEnv("HOME"); ok && strings.TrimSpace(value) != "" {
			return "HOME"
		}
	}
	return "unknown"
}

func (db *DB) openCodexBlockedDecision(ctx context.Context, projectID string, taskID string, runID string, classification string, blockers []string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	decisionID := "DEC-" + stableShortHash(projectID+"|"+taskID+"|"+runID+"|real-codex-blocked")
	options, err := json.Marshal([]map[string]string{
		{"id": "retry_after_manual_action", "label": "Retry after manual action"},
		{"id": "cancel", "label": "Cancel this task"},
	})
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(map[string]any{"run_id": runID, "classification": classification, "blockers": blockers})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, task_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES (?, ?, ?, 'open', 'Real Codex run requires human decision', ?, ?, ?, ?)`,
		decisionID, projectID, taskID, string(options), string(evidence), now, now,
	); err != nil {
		return err
	}
	inboxID := "INBOX-" + stableShortHash(projectID+"|decision|"+decisionID)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, ?, 'human_decision', 'open', 'decision', ?, ?, 80, ?, ?, ?, ?)`,
		inboxID, projectID, taskID, decisionID, "decision:"+decisionID,
		"Real Codex run requires decision", classification, now, now,
	); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "real_codex_decision_required", map[string]any{
		"task_id":     taskID,
		"run_id":      runID,
		"decision_id": decisionID,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func gitOutputOrUnknown(ctx context.Context, dir string, args ...string) string {
	out := gitOutputOrEmpty(ctx, dir, args...)
	if strings.TrimSpace(out) == "" {
		return "UNKNOWN"
	}
	return strings.TrimSpace(out)
}

func gitOutputOrEmpty(ctx context.Context, dir string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
