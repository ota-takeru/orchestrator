package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type CodexExecRequest struct {
	ProjectRoot           string
	Prompt                string
	OutputLastMessagePath string
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

	args := codexExecArgv(request.ProjectRoot, request.Prompt, finalPath)
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

func codexExecArgv(projectRoot string, prompt string, finalPath string) []string {
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
	env, err := db.primaryEnvironment(ctx, projectID)
	if err != nil {
		return RealCodexRunResult{}, err
	}
	if env.OSFamily != platform.OSFamilyLinux || env.ID != "linux-main" {
		return RealCodexRunResult{}, fmt.Errorf("real Codex adapter v1 only supports linux-main current environment")
	}
	if env.CodexAdapter != platform.CodexAdapterLinux {
		return RealCodexRunResult{}, fmt.Errorf("real Codex adapter v1 requires codex-linux environment")
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
	headCommit := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "HEAD")
	diff := gitOutputOrEmpty(ctx, env.ProjectRoot, "diff", "--binary")
	diffHash := sha256Hex([]byte(diff))
	classification, blockers := classifyCodexExecResult(execResult)
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
		Argv:          codexExecArgv(env.ProjectRoot, "<prompt>", "<final-message-path>"),
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
		"task_id":        taskID,
		"run_id":         runID,
		"status":         runStatus,
		"classification": classification,
		"blockers":       blockers,
		"exit_code":      execResult.ExitCode,
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
