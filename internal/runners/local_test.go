package runners

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestLocalRunnerRunsDirectCommand(t *testing.T) {
	runner := NewLocalRunner(platform.ExecutionEnvironment{
		ID:             "linux-main",
		OSFamily:       platform.OSFamilyLinux,
		Shell:          platform.ShellBash,
		ProjectRoot:    t.TempDir(),
		GitProvider:    platform.GitProviderLinux,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
	})
	result, err := runner.RunCommand(context.Background(), RunCommandRequest{
		EnvironmentID:   "linux-main",
		CWD:             runner.environment.ProjectRoot,
		Argv:            []string{"git", "--version"},
		CaptureStdout:   true,
		CaptureStderr:   true,
		NetworkPolicy:   NetworkOff,
		ShellInvocation: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != CommandSucceeded || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
