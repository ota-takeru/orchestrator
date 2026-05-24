package projecthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Tasks(ctx context.Context, project registry.RegisteredProject) (any, error)
	Inbox(ctx context.Context, project registry.RegisteredProject, status string) (any, error)
	CreateFeatureRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error)
	CreateChangeRequest(ctx context.Context, project registry.RegisteredProject, text string) (any, error)
	ApproveInboxItem(ctx context.Context, project registry.RegisteredProject, inboxID string, option string, notes string) (any, error)
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
	return NewHub(reg, WindowsLocalAuthority{}, NewWslAuthority(DefaultCommandExecutor{}, 30*time.Second))
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
