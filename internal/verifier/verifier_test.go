package verifier

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/runners"
)

func TestHybridVerificationFlowSupportsMultipleEnvironments(t *testing.T) {
	registry := StaticRunnerRegistry{
		"windows-main": runners.NewFakeWindowsRunner("windows-main"),
		"wsl-sidecar":  runners.NewFakeWSLRunner("wsl-sidecar"),
	}
	report, err := Run(context.Background(), "RUN-001", registry, []Command{
		{
			ID:               "windows-test",
			EnvironmentID:    "windows-main",
			Runner:           "fake",
			WorkingDir:       `C:\repo`,
			Argv:             []string{"test"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		},
		{
			ID:               "wsl-test",
			EnvironmentID:    "wsl-sidecar",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"test"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("result count = %d", len(report.Results))
	}
	if report.RequiredFailureCount() != 0 || report.OptionalFailureCount() != 0 {
		t.Fatalf("unexpected failures: %#v", report)
	}
}

func TestOptionalVerificationFailureDoesNotCountAsRequiredFailure(t *testing.T) {
	registry := StaticRunnerRegistry{
		"linux-main": runners.NewFakeLinuxRunner("linux-main"),
	}
	report, err := Run(context.Background(), "RUN-001", registry, []Command{
		{
			ID:               "optional-smoke",
			EnvironmentID:    "linux-main",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"fail"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.RequiredFailureCount() != 0 {
		t.Fatalf("optional failure counted as required: %#v", report)
	}
	if report.OptionalFailureCount() != 1 {
		t.Fatalf("optional failure count = %d", report.OptionalFailureCount())
	}
}

func TestCommandFailureIsCurrentDiffFailure(t *testing.T) {
	registry := StaticRunnerRegistry{
		"linux-main": runners.NewFakeLinuxRunner("linux-main"),
	}
	report, err := Run(context.Background(), "RUN-001", registry, []Command{
		{
			ID:               "go-test",
			EnvironmentID:    "linux-main",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"fail"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].FailureClass == nil || *report.Results[0].FailureClass != FailureCurrentDiff {
		t.Fatalf("failure class = %#v", report.Results[0].FailureClass)
	}
}

func TestMissingRunnerIsEnvironmentFailure(t *testing.T) {
	report, err := Run(context.Background(), "RUN-001", StaticRunnerRegistry{}, []Command{
		{
			ID:               "missing-env",
			EnvironmentID:    "wsl-sidecar",
			Runner:           "fake",
			WorkingDir:       "/repo",
			Argv:             []string{"test"},
			NetworkPolicy:    runners.NetworkOff,
			RequiredForMerge: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.RequiredFailureCount() != 1 {
		t.Fatalf("required failure count = %d", report.RequiredFailureCount())
	}
	if report.Results[0].FailureClass == nil || *report.Results[0].FailureClass != FailureEnvironment {
		t.Fatalf("failure class = %#v", report.Results[0].FailureClass)
	}
}
