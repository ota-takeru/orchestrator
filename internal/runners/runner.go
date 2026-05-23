package runners

import (
	"context"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type NetworkPolicy string

const (
	NetworkOff                 NetworkPolicy = "off"
	NetworkAllowlisted         NetworkPolicy = "allowlisted"
	NetworkPackageRegistryOnly NetworkPolicy = "package_registry_only"
	NetworkUnrestricted        NetworkPolicy = "unrestricted"
)

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandRunning   CommandStatus = "running"
	CommandSucceeded CommandStatus = "succeeded"
	CommandFailed    CommandStatus = "failed"
	CommandTimedOut  CommandStatus = "timed_out"
	CommandBlocked   CommandStatus = "blocked"
	CommandCancelled CommandStatus = "cancelled"
)

type Capabilities struct {
	EnvironmentID              string                    `json:"environment_id"`
	Shells                     []platform.Shell          `json:"shells"`
	DirectExec                 bool                      `json:"direct_exec"`
	ShellExec                  bool                      `json:"shell_exec"`
	SupportsTimeout            bool                      `json:"supports_timeout"`
	SupportsProcessGroupCancel bool                      `json:"supports_process_group_cancel"`
	SupportsRedaction          bool                      `json:"supports_redaction"`
	SupportsNetworkPolicy      bool                      `json:"supports_network_policy"`
	PathStyle                  string                    `json:"path_style"`
	SandboxProfiles            []platform.SandboxProfile `json:"sandbox_profiles"`
	GitProviders               []platform.GitProvider    `json:"git_providers"`
}

type PreflightReport struct {
	EnvironmentID string   `json:"environment_id"`
	Ready         bool     `json:"ready"`
	Messages      []string `json:"messages,omitempty"`
}

type RunCommandRequest struct {
	EnvironmentID      string        `json:"environment_id"`
	Runner             string        `json:"runner"`
	CWD                string        `json:"cwd"`
	Argv               []string      `json:"argv"`
	Timeout            time.Duration `json:"timeout"`
	NetworkPolicy      NetworkPolicy `json:"network_policy"`
	EnvBindingIDs      []string      `json:"env_binding_ids,omitempty"`
	CaptureStdout      bool          `json:"capture_stdout"`
	CaptureStderr      bool          `json:"capture_stderr"`
	RedactionRequired  bool          `json:"redaction_required"`
	ShellInvocation    bool          `json:"shell_invocation"`
	CommandKind        string        `json:"command_kind"`
	CommandEventIDHint string        `json:"command_event_id_hint,omitempty"`
}

type RunCommandResult struct {
	EnvironmentID   string        `json:"environment_id"`
	ExitCode        int           `json:"exit_code"`
	Status          CommandStatus `json:"status"`
	StartedAt       time.Time     `json:"started_at"`
	CompletedAt     time.Time     `json:"completed_at"`
	Stdout          string        `json:"stdout,omitempty"`
	Stderr          string        `json:"stderr,omitempty"`
	CommandEventIDs []string      `json:"command_event_ids,omitempty"`
	DetectedRisks   []string      `json:"detected_risks,omitempty"`
}

type ArtifactCollectionRequest struct {
	EnvironmentID string   `json:"environment_id"`
	RunID         string   `json:"run_id"`
	Paths         []string `json:"paths"`
}

type RunArtifact struct {
	Key         string `json:"key"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

type Runner interface {
	EnvironmentID() string
	Capabilities(ctx context.Context) (Capabilities, error)
	Preflight(ctx context.Context) (PreflightReport, error)
	RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error)
	CollectArtifacts(ctx context.Context, req ArtifactCollectionRequest) ([]RunArtifact, error)
}
