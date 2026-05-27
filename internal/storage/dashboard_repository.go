package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

type SetupStatus struct {
	ProjectRoot                    string                       `json:"project_root"`
	GitRepository                  bool                         `json:"git_repository"`
	GitClean                       bool                         `json:"git_clean"`
	GitDirtyFiles                  []string                     `json:"git_dirty_files,omitempty"`
	GitignoreEnvLocal              bool                         `json:"gitignore_env_local"`
	RequiredVerificationConfigured bool                         `json:"required_verification_configured"`
	ProtectedPaths                 []string                     `json:"protected_paths"`
	EnvironmentBindings            []EnvBindingRecord           `json:"environment_bindings"`
	ToolchainSetupCards            []ToolchainSetupInstructions `json:"toolchain_setup_cards"`
	Actions                        []SetupAction                `json:"actions"`
	Blockers                       []string                     `json:"blockers,omitempty"`
}

type SetupAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

type SetupActionResult struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Result   any    `json:"result,omitempty"`
}

type ProjectDashboardData struct {
	Snapshot            HumanInboxSnapshot             `json:"snapshot"`
	Tasks               []TaskRecord                   `json:"tasks"`
	FeatureRequests     []FeatureRequestRecord         `json:"feature_requests"`
	QueueItems          []WorkQueueItemRecord          `json:"queue_items"`
	WorkStatus          WorkStatus                     `json:"work_status"`
	PlanningStatus      PlanningStatus                 `json:"planning_status"`
	ChangeRequests      []ChangeRequestRecord          `json:"change_requests"`
	DependencyRisks     []DependencyRiskRecord         `json:"dependency_risks"`
	Decisions           []DecisionRecord               `json:"decisions"`
	BaselineIssues      []MemoryRecord                 `json:"baseline_issues"`
	Artifacts           []ArtifactRecord               `json:"artifacts"`
	TrustedArtifacts    []TrustedArtifactContentRecord `json:"trusted_artifacts"`
	PathMappings        []PathMappingRecord            `json:"path_mappings"`
	ToolchainSetupCards []ToolchainSetupInstructions   `json:"toolchain_setup_cards"`
	MergeStatus         MergeGateStatus                `json:"merge_status"`
	ProjectViolations   []InvariantViolation           `json:"project_violations"`
	SetupStatus         SetupStatus                    `json:"setup_status"`
}

type TaskRunArtifact struct {
	ID              string `json:"id"`
	RunID           string `json:"run_id"`
	RunType         string `json:"run_type"`
	RunStatus       string `json:"run_status"`
	TaskID          string `json:"task_id"`
	ArtifactType    string `json:"artifact_type"`
	ArtifactKey     string `json:"artifact_key"`
	Path            string `json:"path"`
	ContentHash     string `json:"content_hash"`
	RedactionStatus string `json:"redaction_status"`
	Content         string `json:"content,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func (db *DB) LoadProjectDashboard(ctx context.Context, projectID string, limit int) (ProjectDashboardData, error) {
	if limit <= 0 {
		limit = 20
	}
	snapshot, err := db.LoadHumanInboxSnapshot(ctx, projectID, limit)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	tasks, err := db.ListTasks(ctx, projectID, "")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	requests, err := db.ListFeatureRequests(ctx, projectID, "")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	queue, err := db.ListWorkQueueItems(ctx, projectID, "")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	work, err := db.GetWorkStatus(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	planning, err := db.GetPlanningStatus(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	changes, err := db.ListChangeRequests(ctx, projectID, "")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	risks, err := db.ListDependencyRisks(ctx, DependencyRiskListFilter{ProjectID: projectID})
	if err != nil {
		return ProjectDashboardData{}, err
	}
	decisions, err := db.ListDecisions(ctx, projectID, "open")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	baseline, err := db.ListMemories(ctx, projectID, "baseline_issue")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	artifacts, err := db.ListArtifacts(ctx, projectID, "")
	if err != nil {
		return ProjectDashboardData{}, err
	}
	trusted, err := db.TrustedArtifactContentBundle(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	mappings, err := db.ListPathMappings(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	cards, err := db.ListToolchainSetupCards(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	mergeStatus, err := db.MergeGateStatus(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	violations, err := db.CheckProjectInvariants(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	setup, err := db.LoadSetupStatus(ctx, projectID)
	if err != nil {
		return ProjectDashboardData{}, err
	}
	return ProjectDashboardData{
		Snapshot:            snapshot,
		Tasks:               tasks,
		FeatureRequests:     requests,
		QueueItems:          queue,
		WorkStatus:          work,
		PlanningStatus:      planning,
		ChangeRequests:      changes,
		DependencyRisks:     risks,
		Decisions:           decisions,
		BaselineIssues:      baseline,
		Artifacts:           artifacts,
		TrustedArtifacts:    trusted,
		PathMappings:        mappings,
		ToolchainSetupCards: cards,
		MergeStatus:         mergeStatus,
		ProjectViolations:   violations,
		SetupStatus:         setup,
	}, nil
}

func (db *DB) LoadSetupStatus(ctx context.Context, projectID string) (SetupStatus, error) {
	var root string
	if err := db.sql.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
		return SetupStatus{}, err
	}
	status := SetupStatus{
		ProjectRoot:       root,
		ProtectedPaths:    []string{".env.local", ".env.*", ".devagent-worktrees/", "orchestrator-data/"},
		GitRepository:     pathExists(filepath.Join(root, ".git")),
		GitClean:          true,
		GitignoreEnvLocal: gitignoreCoversEnvLocal(root),
	}
	if dirty := gitDirtyFiles(ctx, root); len(dirty) > 0 {
		status.GitClean = false
		status.GitDirtyFiles = dirty
		status.Blockers = append(status.Blockers, "git worktree is dirty")
	}
	status.RequiredVerificationConfigured = db.projectHasRequiredVerification(ctx, projectID)
	if !status.RequiredVerificationConfigured {
		status.Blockers = append(status.Blockers, "required verification command is not configured")
	}
	if !status.GitRepository {
		status.Blockers = append(status.Blockers, "project root is not a git repository")
	}
	if !status.GitignoreEnvLocal {
		status.Blockers = append(status.Blockers, ".env.local is not ignored")
	}
	bindings, err := db.ListEnvBindings(ctx, projectID)
	if err != nil {
		return SetupStatus{}, err
	}
	status.EnvironmentBindings = bindings
	cards, err := db.ListToolchainSetupCards(ctx, projectID)
	if err != nil {
		return SetupStatus{}, err
	}
	status.ToolchainSetupCards = cards
	status.Actions = setupActions(root, status)
	return status, nil
}

func setupActions(root string, status SetupStatus) []SetupAction {
	quotedRoot := shellQuote(root)
	return []SetupAction{
		{
			ID:      "doctor",
			Label:   "Run doctor",
			Command: "devos doctor --project-root " + quotedRoot + " --json",
			Enabled: true,
		},
		{
			ID:      "codex_readiness",
			Label:   "Check Codex runtime",
			Command: "devos platform codex-readiness --project-root " + quotedRoot + " --save --json",
			Enabled: status.GitRepository,
			Reason:  disabledReason(status.GitRepository, "git repository is required"),
		},
		{
			ID:      "fake_workflow",
			Label:   "Run fake workflow",
			Command: "devos bootstrap --adapter fake --project-root " + quotedRoot + " --json \"Fake setup workflow\"",
			Enabled: status.GitRepository && status.GitignoreEnvLocal,
			Reason:  disabledReason(status.GitRepository && status.GitignoreEnvLocal, "git repo and .env.local ignore rule are required"),
		},
		{
			ID:      "real_dry_run",
			Label:   "Preview real Codex",
			Command: "devos run --real-codex --dry-run --project-root " + quotedRoot + " --json TASK-ID",
			Enabled: status.GitRepository && status.RequiredVerificationConfigured && len(status.ToolchainSetupCards) == 0,
			Reason:  disabledReason(status.GitRepository && status.RequiredVerificationConfigured && len(status.ToolchainSetupCards) == 0, "git, required verification, and toolchains must be ready"),
		},
	}
}

func (db *DB) RunSetupAction(ctx context.Context, projectID string, actionID string) (SetupActionResult, error) {
	actionID = strings.TrimSpace(actionID)
	switch actionID {
	case "doctor":
		var root string
		if err := db.sql.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
			return SetupActionResult{}, err
		}
		env := platform.DetectHostEnvironment(root)
		report := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: true, IncludeUI: true})
		if err := db.SaveToolchainReport(ctx, projectID, report); err != nil {
			return SetupActionResult{}, err
		}
		return SetupActionResult{ActionID: actionID, Status: "succeeded", Message: "doctor report saved", Result: report}, nil
	case "codex_readiness":
		report, err := db.CodexRuntimeReadiness(ctx, projectID)
		if err != nil {
			return SetupActionResult{}, err
		}
		items, err := db.SaveCodexRuntimeReadiness(ctx, projectID, report)
		if err != nil {
			return SetupActionResult{}, err
		}
		return SetupActionResult{ActionID: actionID, Status: "succeeded", Message: "codex readiness saved", Result: map[string]any{"report": report, "inbox_items": items}}, nil
	case "fake_workflow", "real_dry_run":
		return SetupActionResult{ActionID: actionID, Status: "manual_required", Message: "run the displayed command from the project root"}, nil
	default:
		return SetupActionResult{}, fmt.Errorf("unknown setup action: %s", actionID)
	}
}

func disabledReason(enabled bool, reason string) string {
	if enabled {
		return ""
	}
	return reason
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (db *DB) ListTaskRunArtifacts(ctx context.Context, projectID string, taskID string, includeContent bool) ([]TaskRunArtifact, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT ra.id, ra.run_id, r.run_type, r.status, COALESCE(r.task_id, ''),
       ra.artifact_type, ra.artifact_key, ra.path, ra.content_hash,
       ra.redaction_status, ra.created_at
FROM run_artifacts ra
JOIN runs r ON r.project_id = ra.project_id AND r.id = ra.run_id
WHERE ra.project_id = ? AND r.task_id = ?
ORDER BY r.created_at DESC, ra.artifact_type, ra.artifact_key`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []TaskRunArtifact
	for rows.Next() {
		var artifact TaskRunArtifact
		if err := rows.Scan(&artifact.ID, &artifact.RunID, &artifact.RunType, &artifact.RunStatus, &artifact.TaskID, &artifact.ArtifactType, &artifact.ArtifactKey, &artifact.Path, &artifact.ContentHash, &artifact.RedactionStatus, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		if includeContent && safeArtifactContentType(artifact.ArtifactType) {
			content, err := db.readRunArtifactContent(artifact.Path)
			if err == nil {
				artifact.Content = content
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (db *DB) readRunArtifactContent(relPath string) (string, error) {
	clean := filepath.Clean(relPath)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid artifact path")
	}
	content, err := os.ReadFile(filepath.Join(db.dataRoot, clean))
	if err != nil {
		return "", err
	}
	if len(content) > 512*1024 {
		content = content[:512*1024]
	}
	return string(content), nil
}

func safeArtifactContentType(artifactType string) bool {
	switch artifactType {
	case "diff", "summary", "final_message", "verification_summary", "gate_result", "review", "command_stdout", "command_stderr":
		return true
	default:
		return false
	}
}

func (db *DB) projectHasRequiredVerification(ctx context.Context, projectID string) bool {
	var raw sql.NullString
	rows, err := db.sql.QueryContext(ctx, "SELECT verification_commands_json FROM tasks WHERE project_id = ?", projectID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		raw = sql.NullString{}
		if err := rows.Scan(&raw); err != nil {
			return false
		}
		if !raw.Valid || strings.TrimSpace(raw.String) == "" {
			continue
		}
		var commands []TaskVerificationCommand
		if err := json.Unmarshal([]byte(raw.String), &commands); err != nil {
			continue
		}
		for _, command := range commands {
			if command.RequiredForMerge {
				return true
			}
		}
	}
	return false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gitignoreCoversEnvLocal(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == ".env.local" || line == ".env.*" || line == ".env*" {
			return true
		}
	}
	return false
}

func gitDirtyFiles(ctx context.Context, root string) []string {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" || len(line) < 4 {
			continue
		}
		files = append(files, strings.TrimSpace(line[3:]))
	}
	return files
}
