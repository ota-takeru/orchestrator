package projecthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/storage"
)

type ProjectSnapshot = storage.HumanInboxSnapshot

type ProjectAuthority interface {
	Snapshot(ctx context.Context, project registry.RegisteredProject) (ProjectSnapshot, error)
	Dashboard(ctx context.Context, project registry.RegisteredProject) (storage.ProjectDashboardData, error)
	Tasks(ctx context.Context, project registry.RegisteredProject) (any, error)
	Inbox(ctx context.Context, project registry.RegisteredProject, status string) (any, error)
	CreateFeatureRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error)
	CreateChangeRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error)
	AnalyzeChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string) (any, error)
	ApproveChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string, option string) (any, error)
	StartWork(ctx context.Context, project registry.RegisteredProject, input storage.WorkStartInput) (any, error)
	Artifacts(ctx context.Context, project registry.RegisteredProject, artifactType string) (any, error)
	ApproveArtifact(ctx context.Context, project registry.RegisteredProject, artifactID string, version int, status string, notes string) (any, error)
	MaterializeTasks(ctx context.Context, project registry.RegisteredProject) (any, error)
	ApproveInboxItem(ctx context.Context, project registry.RegisteredProject, inboxID string, option string, notes string) (any, error)
	SaveEnvBinding(ctx context.Context, project registry.RegisteredProject, input storage.EnvBindingInput) (any, error)
	TaskArtifacts(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error)
	SetupStatus(ctx context.Context, project registry.RegisteredProject) (any, error)
	SetupAction(ctx context.Context, project registry.RegisteredProject, actionID string) (any, error)
	VerifyTask(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error)
	ApproveTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error)
	RejectTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error)
	ApproveTaskMerge(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error)
	RequestDependencyApproval(ctx context.Context, project registry.RegisteredProject, input storage.DependencyApprovalRequestInput) (any, error)
}

type Hub struct {
	Registry *registry.DB
	Windows  ProjectAuthority
	WSL      ProjectAuthority
}

func NewHub(reg *registry.DB, windows ProjectAuthority, wsl ProjectAuthority) *Hub {
	return &Hub{Registry: reg, Windows: windows, WSL: wsl}
}

func NewDefaultHub(reg *registry.DB) *Hub {
	wslAuthority := ProjectAuthority(NewWslAuthority(DefaultCommandExecutor{}, 30*time.Second))
	if strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" {
		wslAuthority = WindowsLocalAuthority{}
	}
	return NewHub(reg, WindowsLocalAuthority{}, wslAuthority)
}

func (h *Hub) Project(ctx context.Context, projectID string) (registry.RegisteredProject, ProjectAuthority, error) {
	if h == nil || h.Registry == nil {
		return registry.RegisteredProject{}, nil, fmt.Errorf("project registry is not configured")
	}
	project, err := h.Registry.GetProject(ctx, projectID)
	if err != nil {
		return registry.RegisteredProject{}, nil, err
	}
	authority, err := h.AuthorityFor(project)
	if err != nil {
		return registry.RegisteredProject{}, nil, err
	}
	return project, authority, nil
}

func (h *Hub) AuthorityFor(project registry.RegisteredProject) (ProjectAuthority, error) {
	switch project.AuthorityRuntime {
	case registry.AuthorityWindows:
		if h.Windows == nil {
			return nil, fmt.Errorf("windows authority is not configured")
		}
		return h.Windows, nil
	case registry.AuthorityWSL:
		if h.WSL == nil {
			return nil, fmt.Errorf("wsl authority is not configured")
		}
		return h.WSL, nil
	default:
		return nil, fmt.Errorf("invalid authority_runtime: %s", project.AuthorityRuntime)
	}
}

func (h *Hub) Refresh(ctx context.Context, projectID string) (registry.RegisteredProject, ProjectSnapshot, error) {
	project, authority, err := h.Project(ctx, projectID)
	if err != nil {
		return registry.RegisteredProject{}, ProjectSnapshot{}, err
	}
	snapshot, err := authority.Snapshot(ctx, project)
	status := registry.StatusActive
	if err != nil {
		status = registry.StatusInvalid
	}
	updated, updateErr := h.Registry.UpdateProjectStatus(ctx, project.ID, status, true)
	if updateErr != nil && err == nil {
		err = updateErr
	}
	return updated, snapshot, err
}

type WindowsLocalAuthority struct{}

func (WindowsLocalAuthority) Snapshot(ctx context.Context, project registry.RegisteredProject) (ProjectSnapshot, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	defer db.Close()
	return db.LoadHumanInboxSnapshot(ctx, projectID, 20)
}

func (WindowsLocalAuthority) Dashboard(ctx context.Context, project registry.RegisteredProject) (storage.ProjectDashboardData, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return storage.ProjectDashboardData{}, err
	}
	defer db.Close()
	return db.LoadProjectDashboard(ctx, projectID, 20)
}

func (WindowsLocalAuthority) Tasks(ctx context.Context, project registry.RegisteredProject) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tasks, err := db.ListTasks(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks}, nil
}

func (WindowsLocalAuthority) Inbox(ctx context.Context, project registry.RegisteredProject, status string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	items, err := db.ListInboxItems(ctx, projectID, status)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items}, nil
}

func (WindowsLocalAuthority) CreateFeatureRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.CreateFeatureRequest(ctx, projectID, text)
}

func (WindowsLocalAuthority) CreateChangeRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.CreateChangeRequest(ctx, projectID, text)
}

func (WindowsLocalAuthority) AnalyzeChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.AnalyzeChangeRequest(ctx, projectID, changeRequestID)
}

func (WindowsLocalAuthority) ApproveChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string, option string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ApproveChangeRequest(ctx, storage.ChangeApproveInput{ProjectID: projectID, ChangeRequestID: changeRequestID, Option: option})
}

func (WindowsLocalAuthority) StartWork(ctx context.Context, project registry.RegisteredProject, input storage.WorkStartInput) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	input.ProjectID = projectID
	return db.StartWork(ctx, input)
}

func (WindowsLocalAuthority) Artifacts(ctx context.Context, project registry.RegisteredProject, artifactType string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	artifacts, err := db.ListArtifactsWithContent(ctx, projectID, artifactType)
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifacts": artifacts}, nil
}

func (WindowsLocalAuthority) ApproveArtifact(ctx context.Context, project registry.RegisteredProject, artifactID string, version int, status string, notes string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ApproveArtifactVersion(ctx, projectID, artifactID, version, status, notes)
}

func (WindowsLocalAuthority) MaterializeTasks(ctx context.Context, project registry.RegisteredProject) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tasks, err := db.MaterializeApprovedTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks}, nil
}

func (WindowsLocalAuthority) ApproveInboxItem(ctx context.Context, project registry.RegisteredProject, inboxID string, option string, notes string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ApproveInboxItem(ctx, storage.InboxApprovalInput{
		ProjectID: projectID,
		InboxID:   inboxID,
		Option:    option,
		Notes:     notes,
	})
}

func (WindowsLocalAuthority) SaveEnvBinding(ctx context.Context, project registry.RegisteredProject, input storage.EnvBindingInput) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	input.ProjectID = projectID
	return db.SaveEnvBinding(ctx, input)
}

func (WindowsLocalAuthority) TaskArtifacts(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	artifacts, err := db.ListTaskRunArtifacts(ctx, projectID, taskID, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifacts": artifacts}, nil
}

func (WindowsLocalAuthority) SetupStatus(ctx context.Context, project registry.RegisteredProject) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.LoadSetupStatus(ctx, projectID)
}

func (WindowsLocalAuthority) SetupAction(ctx context.Context, project registry.RegisteredProject, actionID string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.RunSetupAction(ctx, projectID, actionID)
}

func (WindowsLocalAuthority) VerifyTask(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.VerifyTask(ctx, projectID, taskID, storage.VerifyTaskInput{Adapter: "local"})
}

func (WindowsLocalAuthority) ApproveTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ApproveTaskEvidence(ctx, storage.ApprovalInput{ProjectID: projectID, TaskID: taskID, ApprovalType: storage.ApprovalFinalReview, Notes: notes})
}

func (WindowsLocalAuthority) RejectTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.RejectTaskFinalReview(ctx, storage.ApprovalInput{ProjectID: projectID, TaskID: taskID, Notes: notes})
}

func (WindowsLocalAuthority) ApproveTaskMerge(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ApproveTaskEvidence(ctx, storage.ApprovalInput{ProjectID: projectID, TaskID: taskID, ApprovalType: storage.ApprovalMerge, Notes: notes})
}

func (WindowsLocalAuthority) RequestDependencyApproval(ctx context.Context, project registry.RegisteredProject, input storage.DependencyApprovalRequestInput) (any, error) {
	db, projectID, err := openProjectDB(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	input.ProjectID = projectID
	return db.RequestDependencyApproval(ctx, input)
}

func openProjectDB(ctx context.Context, project registry.RegisteredProject) (*storage.DB, string, error) {
	root := strings.TrimSpace(project.ProjectRoot)
	if root == "" {
		return nil, "", fmt.Errorf("project_root is required")
	}
	dataRoot := strings.TrimSpace(project.DataRoot)
	if dataRoot == "" {
		dataRoot = filepath.Join(root, "orchestrator-data")
	}
	db, err := storage.Open(ctx, filepath.Join(dataRoot, "devos.sqlite"))
	if err != nil {
		return nil, "", err
	}
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		_ = db.Close()
		return nil, "", err
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	return db, storage.ProjectIDForRoot(root), nil
}

type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error)
}

type StdinCommandExecutor interface {
	RunWithInput(ctx context.Context, stdin string, name string, args ...string) ([]byte, []byte, int, error)
}

type DefaultCommandExecutor struct{}

func (DefaultCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.Output()
	var stderr []byte
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = exitErr.Stderr
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout, stderr, exitCode, err
}

func (DefaultCommandExecutor) RunWithInput(ctx context.Context, stdin string, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	stdout, err := cmd.Output()
	var stderr []byte
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = exitErr.Stderr
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout, stderr, exitCode, err
}

type AuthorityError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

func (e *AuthorityError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Stderr)
	}
	return e.Message
}

type WslAuthority struct {
	executor CommandExecutor
	timeout  time.Duration
}

func NewWslAuthority(executor CommandExecutor, timeout time.Duration) WslAuthority {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return WslAuthority{executor: executor, timeout: timeout}
}

func (a WslAuthority) Snapshot(ctx context.Context, project registry.RegisteredProject) (ProjectSnapshot, error) {
	var snapshot ProjectSnapshot
	if err := a.runJSON(ctx, project, &snapshot, "ui", "snapshot"); err != nil {
		return ProjectSnapshot{}, err
	}
	return snapshot, nil
}

func (a WslAuthority) Dashboard(ctx context.Context, project registry.RegisteredProject) (storage.ProjectDashboardData, error) {
	var dashboard storage.ProjectDashboardData
	if err := a.runJSON(ctx, project, &dashboard, "ui", "dashboard"); err != nil {
		return storage.ProjectDashboardData{}, err
	}
	return dashboard, nil
}

func (a WslAuthority) Tasks(ctx context.Context, project registry.RegisteredProject) (any, error) {
	var body map[string]any
	if err := a.runJSON(ctx, project, &body, "tasks"); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) Inbox(ctx context.Context, project registry.RegisteredProject, status string) (any, error) {
	var body map[string]any
	args := []string{"inbox"}
	if strings.TrimSpace(status) != "" {
		args = append(args, "--status", status)
	}
	if err := a.runJSON(ctx, project, &body, args...); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) CreateFeatureRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"request"}, text); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) CreateChangeRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"change", "request"}, text); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) AnalyzeChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"change", "analyze"}, changeRequestID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) ApproveChangeRequest(ctx context.Context, project registry.RegisteredProject, changeRequestID string, option string) (any, error) {
	var body map[string]any
	args := []string{"change", "approve"}
	if strings.TrimSpace(option) != "" {
		args = append(args, "--option", option)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, changeRequestID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) StartWork(ctx context.Context, project registry.RegisteredProject, input storage.WorkStartInput) (any, error) {
	var body map[string]any
	args := []string{"work", "start", "--mode", "sequential", "--implementation-concurrency", "1"}
	if strings.TrimSpace(input.ImplementationAdapter) != "" {
		args = append(args, "--adapter", input.ImplementationAdapter)
	}
	if input.PlanningConcurrency > 0 {
		args = append(args, "--planning-concurrency", fmt.Sprintf("%d", input.PlanningConcurrency))
	}
	if strings.TrimSpace(input.Until) != "" {
		args = append(args, "--until", input.Until)
	}
	if err := a.runJSON(ctx, project, &body, args...); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) Artifacts(ctx context.Context, project registry.RegisteredProject, artifactType string) (any, error) {
	var body map[string]any
	args := []string{"artifacts", "--include-content"}
	if strings.TrimSpace(artifactType) != "" {
		args = append(args, "--type", artifactType)
	}
	if err := a.runJSON(ctx, project, &body, args...); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) ApproveArtifact(ctx context.Context, project registry.RegisteredProject, artifactID string, version int, status string, notes string) (any, error) {
	var body map[string]any
	args := []string{"artifacts", "approve", "--version", fmt.Sprintf("%d", version)}
	if strings.TrimSpace(status) != "" {
		args = append(args, "--status", status)
	}
	if strings.TrimSpace(notes) != "" {
		args = append(args, "--notes", notes)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, artifactID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) MaterializeTasks(ctx context.Context, project registry.RegisteredProject) (any, error) {
	var body map[string]any
	if err := a.runJSON(ctx, project, &body, "tasks", "materialize"); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) ApproveInboxItem(ctx context.Context, project registry.RegisteredProject, inboxID string, option string, notes string) (any, error) {
	var body map[string]any
	args := []string{"inbox", "approve"}
	if option != "" {
		args = append(args, "--option", option)
	}
	if notes != "" {
		args = append(args, "--notes", notes)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, inboxID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) SaveEnvBinding(ctx context.Context, project registry.RegisteredProject, input storage.EnvBindingInput) (any, error) {
	executor, ok := a.executor.(StdinCommandExecutor)
	if !ok {
		return nil, &AuthorityError{Code: "wsl_stdin_missing", Message: "wsl command executor does not support stdin for secret input"}
	}
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = "project"
	}
	args := []string{"env", "set", "--scope", scope, "--value-stdin"}
	if strings.TrimSpace(input.EnvironmentID) != "" {
		args = append(args, "--env", input.EnvironmentID)
	}
	if strings.TrimSpace(input.ScopeID) != "" {
		args = append(args, "--scope-id", input.ScopeID)
	}
	base, err := a.commandArgs(project, args, input.Key)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	stdout, stderr, exitCode, runErr := executor.RunWithInput(runCtx, input.Value, "wsl.exe", base...)
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, &AuthorityError{Code: "wsl_timeout", Message: "wsl authority command timed out"}
	}
	if runErr != nil {
		return nil, &AuthorityError{Code: "wsl_command_failed", Message: runErr.Error(), ExitCode: exitCode, Stderr: strings.TrimSpace(string(stderr))}
	}
	var body map[string]any
	if err := json.Unmarshal(stdout, &body); err != nil {
		return nil, &AuthorityError{Code: "wsl_invalid_json", Message: err.Error(), Stderr: strings.TrimSpace(string(stdout))}
	}
	return body, nil
}

func (a WslAuthority) TaskArtifacts(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"task", "artifacts", "--include-content"}, taskID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) SetupStatus(ctx context.Context, project registry.RegisteredProject) (any, error) {
	var body map[string]any
	if err := a.runJSON(ctx, project, &body, "ui", "setup"); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) SetupAction(ctx context.Context, project registry.RegisteredProject, actionID string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"ui", "setup-action"}, actionID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) VerifyTask(ctx context.Context, project registry.RegisteredProject, taskID string) (any, error) {
	var body map[string]any
	if err := a.runJSONWithTrailing(ctx, project, &body, []string{"verify"}, taskID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) ApproveTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	var body map[string]any
	args := []string{"review", "approve"}
	if notes != "" {
		args = append(args, "--notes", notes)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, taskID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) RejectTaskReview(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	var body map[string]any
	args := []string{"review", "reject"}
	if notes != "" {
		args = append(args, "--notes", notes)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, taskID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) ApproveTaskMerge(ctx context.Context, project registry.RegisteredProject, taskID string, notes string) (any, error) {
	var body map[string]any
	args := []string{"merge", "approve"}
	if notes != "" {
		args = append(args, "--notes", notes)
	}
	if err := a.runJSONWithTrailing(ctx, project, &body, args, taskID); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) RequestDependencyApproval(ctx context.Context, project registry.RegisteredProject, input storage.DependencyApprovalRequestInput) (any, error) {
	var body map[string]any
	args := []string{
		"dependency", "approval", "request",
		"--name", input.Name,
		"--manager", input.PackageManager,
		"--type", input.DependencyType,
		"--reason", input.Reason,
		"--risk", input.Risk,
	}
	if input.Alternatives != "" {
		args = append(args, "--alternatives", input.Alternatives)
	}
	if input.FilesAffected != "" {
		args = append(args, "--files-affected", input.FilesAffected)
	}
	if input.LifecycleScripts != "" {
		args = append(args, "--lifecycle-scripts", input.LifecycleScripts)
	}
	if input.CurrentVersion != "" {
		args = append(args, "--current-version", input.CurrentVersion)
	}
	if input.ApprovedScope != "" {
		args = append(args, "--approved-scope", input.ApprovedScope)
	}
	if input.IntroducedTaskID != "" {
		args = append(args, "--task", input.IntroducedTaskID)
	}
	if input.IntroducedRunID != "" {
		args = append(args, "--run", input.IntroducedRunID)
	}
	if err := a.runJSON(ctx, project, &body, args...); err != nil {
		return nil, err
	}
	return body, nil
}

func (a WslAuthority) runJSON(ctx context.Context, project registry.RegisteredProject, target any, command ...string) error {
	return a.runJSONWithTrailing(ctx, project, target, command)
}

func (a WslAuthority) runJSONWithTrailing(ctx context.Context, project registry.RegisteredProject, target any, command []string, trailing ...string) error {
	if a.executor == nil {
		return &AuthorityError{Code: "wsl_executor_missing", Message: "wsl command executor is not configured"}
	}
	base, err := a.commandArgs(project, command, trailing...)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	stdout, stderr, exitCode, runErr := a.executor.Run(runCtx, "wsl.exe", base...)
	if runCtx.Err() == context.DeadlineExceeded {
		return &AuthorityError{Code: "wsl_timeout", Message: "wsl authority command timed out"}
	}
	if runErr != nil {
		return &AuthorityError{Code: "wsl_command_failed", Message: runErr.Error(), ExitCode: exitCode, Stderr: strings.TrimSpace(string(stderr))}
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return &AuthorityError{Code: "wsl_invalid_json", Message: err.Error(), Stderr: strings.TrimSpace(string(stdout))}
	}
	return nil
}

func (a WslAuthority) commandArgs(project registry.RegisteredProject, command []string, trailing ...string) ([]string, error) {
	distro := strings.TrimSpace(project.WSLDistro)
	root := strings.TrimSpace(project.WSLProjectRoot)
	if distro == "" {
		return nil, &AuthorityError{Code: "wsl_project_invalid", Message: "wsl_distro is required"}
	}
	if root == "" {
		return nil, &AuthorityError{Code: "wsl_project_invalid", Message: "wsl_project_root is required"}
	}
	if looksWindowsPath(root) {
		return nil, &AuthorityError{Code: "wsl_project_invalid", Message: "wsl_project_root must be a Linux path, not a Windows path"}
	}
	// TODO: replace this command bridge with `devos agent --stdio` once the WSL authority daemon exists.
	args := []string{"-d", distro, "--", "devos"}
	args = append(args, command...)
	args = append(args, "--project-root", root)
	dataRoot := strings.TrimSpace(project.DataRoot)
	if dataRoot != "" {
		if looksWindowsPath(dataRoot) {
			return nil, &AuthorityError{Code: "wsl_project_invalid", Message: "data_root for WSL authority must be a Linux path"}
		}
		args = append(args, "--data-root", dataRoot)
	}
	args = append(args, "--json")
	args = append(args, trailing...)
	return args, nil
}

func looksWindowsPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	if strings.HasPrefix(lower, `\\wsl$`) || strings.HasPrefix(lower, `\\wsl.localhost`) {
		return true
	}
	return len(lower) >= 3 && lower[1] == ':' && (lower[2] == '\\' || lower[2] == '/')
}
