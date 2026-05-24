package platform

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type OSFamily string

const (
	OSFamilyWindows       OSFamily = "windows"
	OSFamilyWSL           OSFamily = "wsl"
	OSFamilyLinux         OSFamily = "linux"
	OSFamilyMacOS         OSFamily = "macos"
	OSFamilyRemoteWindows OSFamily = "remote_windows"
	OSFamilyRemoteLinux   OSFamily = "remote_linux"
)

type Role string

const (
	RolePrimary  Role = "primary"
	RoleSidecar  Role = "sidecar"
	RoleRemote   Role = "remote"
	RoleDisabled Role = "disabled"
)

type Shell string

const (
	ShellPowerShell Shell = "powershell"
	ShellCmd        Shell = "cmd"
	ShellBash       Shell = "bash"
	ShellSh         Shell = "sh"
	ShellNone       Shell = "none"
)

type GitProvider string

const (
	GitProviderWindows GitProvider = "git-for-windows"
	GitProviderLinux   GitProvider = "linux-git"
	GitProviderNone    GitProvider = "none"
)

type CodexAdapter string

const (
	CodexAdapterWindows CodexAdapter = "codex-windows"
	CodexAdapterWSL     CodexAdapter = "codex-wsl"
	CodexAdapterLinux   CodexAdapter = "codex-linux"
	CodexAdapterNone    CodexAdapter = "none"
)

type SandboxProfile string

const (
	SandboxWindowsNative   SandboxProfile = "windows-native"
	SandboxLinuxBubblewrap SandboxProfile = "linux-bubblewrap"
	SandboxExternal        SandboxProfile = "external-isolated"
	SandboxNone            SandboxProfile = "none"
)

type PlatformMode string

const (
	PlatformModeSingleEnvironment PlatformMode = "single_environment"
	PlatformModeWindowsPrimary    PlatformMode = "windows_primary"
	PlatformModeWSLPrimary        PlatformMode = "wsl_primary"
	PlatformModeHybrid            PlatformMode = "hybrid"
)

type MappingMode string

const (
	MappingSameFilesystem   MappingMode = "same_filesystem"
	MappingIsolatedWorktree MappingMode = "isolated_worktree"
	MappingMirroredClone    MappingMode = "mirrored_clone"
	MappingUnsupported      MappingMode = "unsupported"
)

type ExecutionEnvironment struct {
	ID             string         `json:"id"`
	OSFamily       OSFamily       `json:"os_family"`
	Role           Role           `json:"role"`
	Shell          Shell          `json:"shell"`
	ProjectRoot    string         `json:"project_root"`
	WorktreeRoot   string         `json:"worktree_root,omitempty"`
	GitProvider    GitProvider    `json:"git_provider"`
	CodexAdapter   CodexAdapter   `json:"codex_adapter"`
	SandboxProfile SandboxProfile `json:"sandbox_profile"`
	Status         string         `json:"status"`
}

type RunProfile struct {
	Mode                             PlatformMode `json:"mode"`
	PrimaryEnvironmentID             string       `json:"primary_environment_id"`
	ImplementationEnvironmentID      string       `json:"implementation_environment_id"`
	MergeEnvironmentID               string       `json:"merge_environment_id"`
	RequiredVerificationEnvironments []string     `json:"required_verification_environment_ids"`
	OptionalVerificationEnvironments []string     `json:"optional_verification_environment_ids"`
}

func ValidOSFamily(v OSFamily) bool {
	switch v {
	case OSFamilyWindows, OSFamilyWSL, OSFamilyLinux, OSFamilyMacOS, OSFamilyRemoteWindows, OSFamilyRemoteLinux:
		return true
	default:
		return false
	}
}

func ValidRole(v Role) bool {
	switch v {
	case RolePrimary, RoleSidecar, RoleRemote, RoleDisabled:
		return true
	default:
		return false
	}
}

func ValidShell(v Shell) bool {
	switch v {
	case ShellPowerShell, ShellCmd, ShellBash, ShellSh, ShellNone:
		return true
	default:
		return false
	}
}

func ValidGitProvider(v GitProvider) bool {
	switch v {
	case GitProviderWindows, GitProviderLinux, GitProviderNone:
		return true
	default:
		return false
	}
}

func ValidCodexAdapter(v CodexAdapter) bool {
	switch v {
	case CodexAdapterWindows, CodexAdapterWSL, CodexAdapterLinux, CodexAdapterNone:
		return true
	default:
		return false
	}
}

func ValidSandboxProfile(v SandboxProfile) bool {
	switch v {
	case SandboxWindowsNative, SandboxLinuxBubblewrap, SandboxExternal, SandboxNone:
		return true
	default:
		return false
	}
}

func ValidPlatformMode(v PlatformMode) bool {
	switch v {
	case PlatformModeSingleEnvironment, PlatformModeWindowsPrimary, PlatformModeWSLPrimary, PlatformModeHybrid:
		return true
	default:
		return false
	}
}

func ValidMappingMode(v MappingMode) bool {
	switch v {
	case MappingSameFilesystem, MappingIsolatedWorktree, MappingMirroredClone, MappingUnsupported:
		return true
	default:
		return false
	}
}

func DetectHostEnvironment(projectRoot string) ExecutionEnvironment {
	return detectHostEnvironment(runtime.GOOS, projectRoot, readLinuxOSRelease())
}

func detectHostEnvironment(goos string, projectRoot string, linuxOSRelease string) ExecutionEnvironment {
	switch goos {
	case "windows":
		return ExecutionEnvironment{
			ID:             "windows-main",
			OSFamily:       OSFamilyWindows,
			Role:           RolePrimary,
			Shell:          ShellPowerShell,
			ProjectRoot:    projectRoot,
			GitProvider:    GitProviderWindows,
			CodexAdapter:   CodexAdapterWindows,
			SandboxProfile: SandboxWindowsNative,
			Status:         "detected",
		}
	case "darwin":
		return ExecutionEnvironment{
			ID:             "macos-main",
			OSFamily:       OSFamilyMacOS,
			Role:           RolePrimary,
			Shell:          ShellBash,
			ProjectRoot:    projectRoot,
			GitProvider:    GitProviderLinux,
			CodexAdapter:   CodexAdapterLinux,
			SandboxProfile: SandboxNone,
			Status:         "detected",
		}
	default:
		if isWSLRelease(linuxOSRelease) {
			return ExecutionEnvironment{
				ID:             "wsl-main",
				OSFamily:       OSFamilyWSL,
				Role:           RolePrimary,
				Shell:          ShellBash,
				ProjectRoot:    projectRoot,
				GitProvider:    GitProviderLinux,
				CodexAdapter:   CodexAdapterWSL,
				SandboxProfile: SandboxLinuxBubblewrap,
				Status:         "detected",
			}
		}
		return ExecutionEnvironment{
			ID:             "linux-main",
			OSFamily:       OSFamilyLinux,
			Role:           RolePrimary,
			Shell:          ShellBash,
			ProjectRoot:    projectRoot,
			GitProvider:    GitProviderLinux,
			CodexAdapter:   CodexAdapterLinux,
			SandboxProfile: SandboxLinuxBubblewrap,
			Status:         "detected",
		}
	}
}

func readLinuxOSRelease() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	raw, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return string(raw)
}

func isWSLRelease(release string) bool {
	lower := strings.ToLower(strings.TrimSpace(release))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func ValidatePrimaryEnvironment(envs []ExecutionEnvironment) error {
	count := 0
	for _, env := range envs {
		if env.Role == RolePrimary {
			count++
		}
		if !ValidOSFamily(env.OSFamily) {
			return fmt.Errorf("invalid os_family for %s: %s", env.ID, env.OSFamily)
		}
		if !ValidRole(env.Role) {
			return fmt.Errorf("invalid role for %s: %s", env.ID, env.Role)
		}
		if !ValidShell(env.Shell) {
			return fmt.Errorf("invalid shell for %s: %s", env.ID, env.Shell)
		}
		if !ValidGitProvider(env.GitProvider) {
			return fmt.Errorf("invalid git_provider for %s: %s", env.ID, env.GitProvider)
		}
		if !ValidCodexAdapter(env.CodexAdapter) {
			return fmt.Errorf("invalid codex_adapter for %s: %s", env.ID, env.CodexAdapter)
		}
		if !ValidSandboxProfile(env.SandboxProfile) {
			return fmt.Errorf("invalid sandbox_profile for %s: %s", env.ID, env.SandboxProfile)
		}
	}
	if count != 1 {
		return fmt.Errorf("project requires exactly one primary environment, got %d", count)
	}
	return nil
}
