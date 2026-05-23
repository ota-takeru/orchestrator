package toolchains

import (
	"context"
	"errors"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestDoctorClassifiesMissingToolchainSeparately(t *testing.T) {
	env := platform.ExecutionEnvironment{
		ID:             "linux-main",
		OSFamily:       platform.OSFamilyLinux,
		Shell:          platform.ShellBash,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterLinux,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
	}
	report := RunDoctor(context.Background(), env, Options{
		IncludeCodex: true,
		LookupPath: func(file string) (string, error) {
			if file == "git" || file == "bash" {
				return "/usr/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
	})

	statuses := map[string]Status{}
	for _, req := range report.Requirements {
		statuses[req.ToolchainKey] = req.Status
	}
	if statuses["git"] != StatusDetected {
		t.Fatalf("git status = %s", statuses["git"])
	}
	if statuses["bash"] != StatusDetected {
		t.Fatalf("bash status = %s", statuses["bash"])
	}
	if statuses["bubblewrap"] != StatusMissing {
		t.Fatalf("bubblewrap status = %s", statuses["bubblewrap"])
	}
	if statuses["codex"] != StatusSetupRequired {
		t.Fatalf("codex status = %s", statuses["codex"])
	}
}

func TestFakeBootstrapDoesNotRequireCodexAuth(t *testing.T) {
	env := platform.ExecutionEnvironment{
		ID:             "linux-main",
		OSFamily:       platform.OSFamilyLinux,
		Shell:          platform.ShellBash,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterLinux,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
	}
	report := RunDoctor(context.Background(), env, Options{
		IncludeCodex: false,
		LookupPath: func(file string) (string, error) {
			if file == "git" || file == "bash" {
				return "/usr/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
	})
	for _, req := range report.Requirements {
		if req.ToolchainKey == "codex" {
			t.Fatal("codex requirement should not be emitted when IncludeCodex=false")
		}
	}
}
