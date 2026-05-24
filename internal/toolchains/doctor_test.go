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
		LookupEnv: func(key string) (string, bool) {
			if key == "HOME" {
				return "/home/dev", true
			}
			return "", false
		},
		FileExists: func(path string) bool {
			return false
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
	if statuses["codex-auth"] != StatusSetupRequired {
		t.Fatalf("codex-auth status = %s", statuses["codex-auth"])
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
		if req.ToolchainKey == "codex" || req.ToolchainKey == "codex-auth" {
			t.Fatalf("%s requirement should not be emitted when IncludeCodex=false", req.ToolchainKey)
		}
	}
}

func TestDoctorDetectsEnvironmentSpecificCodexAuth(t *testing.T) {
	env := platform.ExecutionEnvironment{
		ID:             "wsl-main",
		OSFamily:       platform.OSFamilyWSL,
		Shell:          platform.ShellBash,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
	}
	report := RunDoctor(context.Background(), env, Options{
		IncludeCodex: true,
		LookupPath: func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		},
		LookupEnv: func(key string) (string, bool) {
			if key == "HOME" {
				return "/home/wsl-user", true
			}
			return "", false
		},
		FileExists: func(path string) bool {
			return path == "/home/wsl-user/.codex/auth.json"
		},
	})

	auth := requirementByKey(report, "codex-auth")
	if auth.Status != StatusDetected || auth.DetectedPath != "/home/wsl-user/.codex" {
		t.Fatalf("codex-auth requirement = %#v", auth)
	}
}

func TestDoctorUsesWindowsCodexHomeBoundary(t *testing.T) {
	env := platform.ExecutionEnvironment{
		ID:             "windows-main",
		OSFamily:       platform.OSFamilyWindows,
		Shell:          platform.ShellPowerShell,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
	}
	report := RunDoctor(context.Background(), env, Options{
		IncludeCodex: true,
		LookupPath: func(file string) (string, error) {
			return `C:\bin\` + file + ".exe", nil
		},
		LookupEnv: func(key string) (string, bool) {
			if key == "USERPROFILE" {
				return `C:\Users\dev`, true
			}
			return "", false
		},
		FileExists: func(path string) bool {
			return path == `C:\Users\dev\.codex\auth.json`
		},
	})

	auth := requirementByKey(report, "codex-auth")
	if auth.Status != StatusDetected || auth.DetectedPath != `C:\Users\dev\.codex` {
		t.Fatalf("codex-auth requirement = %#v", auth)
	}
}

func requirementByKey(report Report, key string) Requirement {
	for _, req := range report.Requirements {
		if req.ToolchainKey == key {
			return req
		}
	}
	return Requirement{}
}
