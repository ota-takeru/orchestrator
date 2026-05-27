package runners

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type LocalRunner struct {
	environment platform.ExecutionEnvironment
}

func NewLocalRunner(environment platform.ExecutionEnvironment) LocalRunner {
	return LocalRunner{environment: environment}
}

func (r LocalRunner) EnvironmentID() string {
	return r.environment.ID
}

func (r LocalRunner) Capabilities(ctx context.Context) (Capabilities, error) {
	_ = ctx
	pathStyle := "posix"
	if r.environment.OSFamily == platform.OSFamilyWindows || r.environment.OSFamily == platform.OSFamilyRemoteWindows {
		pathStyle = "windows"
	}
	return Capabilities{
		EnvironmentID:              r.environment.ID,
		Shells:                     []platform.Shell{r.environment.Shell},
		DirectExec:                 true,
		ShellExec:                  false,
		SupportsTimeout:            true,
		SupportsProcessGroupCancel: true,
		SupportsRedaction:          false,
		SupportsNetworkPolicy:      true,
		PathStyle:                  pathStyle,
		SandboxProfiles:            []platform.SandboxProfile{r.environment.SandboxProfile},
		GitProviders:               []platform.GitProvider{r.environment.GitProvider},
	}, nil
}

func (r LocalRunner) Preflight(ctx context.Context) (PreflightReport, error) {
	_ = ctx
	return PreflightReport{EnvironmentID: r.environment.ID, Ready: true}, nil
}

func (r LocalRunner) RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error) {
	started := time.Now().UTC()
	if req.EnvironmentID != r.environment.ID {
		return RunCommandResult{}, fmt.Errorf("runner %s cannot execute for environment %s", r.environment.ID, req.EnvironmentID)
	}
	if len(req.Argv) == 0 {
		return RunCommandResult{}, fmt.Errorf("argv is required")
	}
	commandCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		commandCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()
	argv := resolveLocalArgv(r.environment, req.Argv)
	cmd := exec.CommandContext(commandCtx, argv[0], argv[1:]...)
	cmd.Dir = req.CWD
	var stdout, stderr bytes.Buffer
	if req.CaptureStdout {
		cmd.Stdout = &stdout
	}
	if req.CaptureStderr {
		cmd.Stderr = &stderr
	}
	err := cmd.Run()
	completed := time.Now().UTC()
	result := RunCommandResult{
		EnvironmentID: r.environment.ID,
		Status:        CommandSucceeded,
		StartedAt:     started,
		CompletedAt:   completed,
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		result.Status = CommandTimedOut
		return result, commandCtx.Err()
	}
	if err != nil {
		result.Status = CommandFailed
		return result, nil
	}
	return result, nil
}

func resolveLocalArgv(env platform.ExecutionEnvironment, argv []string) []string {
	if len(argv) == 0 || runtime.GOOS != "windows" || argv[0] != "sh" {
		return argv
	}
	if _, err := exec.LookPath("sh"); err == nil {
		return argv
	}
	for _, candidate := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin", "sh.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
	} {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			resolved := append([]string{candidate}, argv[1:]...)
			return resolved
		}
	}
	return argv
}

func (r LocalRunner) CollectArtifacts(ctx context.Context, req ArtifactCollectionRequest) ([]RunArtifact, error) {
	_ = ctx
	if req.EnvironmentID != r.environment.ID {
		return nil, fmt.Errorf("runner %s cannot collect for environment %s", r.environment.ID, req.EnvironmentID)
	}
	return nil, nil
}
