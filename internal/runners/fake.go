package runners

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type FakeRunner struct {
	environment platform.ExecutionEnvironment
	pathStyle   string
}

func NewFakeWindowsRunner(environmentID string) FakeRunner {
	return FakeRunner{
		environment: platform.ExecutionEnvironment{
			ID:             environmentID,
			OSFamily:       platform.OSFamilyWindows,
			Shell:          platform.ShellPowerShell,
			GitProvider:    platform.GitProviderWindows,
			SandboxProfile: platform.SandboxWindowsNative,
		},
		pathStyle: "windows",
	}
}

func NewFakeWSLRunner(environmentID string) FakeRunner {
	return FakeRunner{
		environment: platform.ExecutionEnvironment{
			ID:             environmentID,
			OSFamily:       platform.OSFamilyWSL,
			Shell:          platform.ShellBash,
			GitProvider:    platform.GitProviderLinux,
			SandboxProfile: platform.SandboxLinuxBubblewrap,
		},
		pathStyle: "posix",
	}
}

func NewFakeLinuxRunner(environmentID string) FakeRunner {
	return FakeRunner{
		environment: platform.ExecutionEnvironment{
			ID:             environmentID,
			OSFamily:       platform.OSFamilyLinux,
			Shell:          platform.ShellBash,
			GitProvider:    platform.GitProviderLinux,
			SandboxProfile: platform.SandboxLinuxBubblewrap,
		},
		pathStyle: "posix",
	}
}

func (r FakeRunner) EnvironmentID() string {
	return r.environment.ID
}

func (r FakeRunner) Capabilities(ctx context.Context) (Capabilities, error) {
	_ = ctx
	return Capabilities{
		EnvironmentID:              r.environment.ID,
		Shells:                     []platform.Shell{r.environment.Shell},
		DirectExec:                 true,
		ShellExec:                  true,
		SupportsTimeout:            true,
		SupportsProcessGroupCancel: true,
		SupportsRedaction:          true,
		SupportsNetworkPolicy:      true,
		PathStyle:                  r.pathStyle,
		SandboxProfiles:            []platform.SandboxProfile{r.environment.SandboxProfile},
		GitProviders:               []platform.GitProvider{r.environment.GitProvider},
	}, nil
}

func (r FakeRunner) Preflight(ctx context.Context) (PreflightReport, error) {
	_ = ctx
	return PreflightReport{EnvironmentID: r.environment.ID, Ready: true}, nil
}

func (r FakeRunner) RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error) {
	started := time.Now().UTC()
	if req.EnvironmentID != r.environment.ID {
		return RunCommandResult{}, fmt.Errorf("runner %s cannot execute for environment %s", r.environment.ID, req.EnvironmentID)
	}
	if err := ctx.Err(); err != nil {
		return RunCommandResult{
			EnvironmentID: r.environment.ID,
			Status:        CommandCancelled,
			StartedAt:     started,
			CompletedAt:   time.Now().UTC(),
		}, err
	}
	status := CommandSucceeded
	exitCode := 0
	stderr := ""
	stdout := "fake command succeeded"
	if containsToken(req.Argv, "fail") {
		status = CommandFailed
		exitCode = 1
		stdout = ""
		stderr = "fake command failed"
	}
	return RunCommandResult{
		EnvironmentID:   r.environment.ID,
		ExitCode:        exitCode,
		Status:          status,
		StartedAt:       started,
		CompletedAt:     time.Now().UTC(),
		Stdout:          stdout,
		Stderr:          stderr,
		CommandEventIDs: []string{req.CommandEventIDHint},
	}, nil
}

func (r FakeRunner) CollectArtifacts(ctx context.Context, req ArtifactCollectionRequest) ([]RunArtifact, error) {
	_ = ctx
	if req.EnvironmentID != r.environment.ID {
		return nil, fmt.Errorf("runner %s cannot collect for environment %s", r.environment.ID, req.EnvironmentID)
	}
	artifacts := make([]RunArtifact, 0, len(req.Paths))
	for i, p := range req.Paths {
		artifacts = append(artifacts, RunArtifact{
			Key:         fmt.Sprintf("artifact-%d", i+1),
			Path:        p,
			ContentHash: "fake",
		})
	}
	return artifacts, nil
}

func containsToken(argv []string, token string) bool {
	for _, arg := range argv {
		if strings.EqualFold(arg, token) {
			return true
		}
	}
	return false
}
