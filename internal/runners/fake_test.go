package runners

import (
	"context"
	"testing"
)

func TestFakeWindowsRunnerCapabilities(t *testing.T) {
	runner := NewFakeWindowsRunner("windows-main")
	capabilities, err := runner.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.EnvironmentID != "windows-main" {
		t.Fatalf("environment id = %s", capabilities.EnvironmentID)
	}
	if capabilities.PathStyle != "windows" {
		t.Fatalf("path style = %s", capabilities.PathStyle)
	}
	if !capabilities.SupportsNetworkPolicy {
		t.Fatal("fake runner should report network policy support")
	}
}

func TestFakeRunnerRunsSuccessfulCommand(t *testing.T) {
	runner := NewFakeWSLRunner("wsl-main")
	result, err := runner.RunCommand(context.Background(), RunCommandRequest{
		EnvironmentID: "wsl-main",
		Runner:        "fake",
		CWD:           "/repo",
		Argv:          []string{"go", "test", "./..."},
		NetworkPolicy: NetworkOff,
		CaptureStdout: true,
		CaptureStderr: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != CommandSucceeded || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFakeRunnerCanReturnFailure(t *testing.T) {
	runner := NewFakeLinuxRunner("linux-main")
	result, err := runner.RunCommand(context.Background(), RunCommandRequest{
		EnvironmentID: "linux-main",
		Runner:        "fake",
		CWD:           "/repo",
		Argv:          []string{"fail"},
		NetworkPolicy: NetworkOff,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != CommandFailed || result.ExitCode != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFakeRunnerRejectsWrongEnvironment(t *testing.T) {
	runner := NewFakeLinuxRunner("linux-main")
	if _, err := runner.RunCommand(context.Background(), RunCommandRequest{EnvironmentID: "other"}); err == nil {
		t.Fatal("expected wrong environment to fail")
	}
}
