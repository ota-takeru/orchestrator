package toolchains

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type RequiredFor string

const (
	RequiredForImplementation RequiredFor = "implementation"
	RequiredForVerification   RequiredFor = "verification"
	RequiredForRuntime        RequiredFor = "runtime"
	RequiredForRuntimeSmoke   RequiredFor = "runtime_smoke"
	RequiredForDeployment     RequiredFor = "deployment"
)

type Status string

const (
	StatusDetected      Status = "detected"
	StatusMissing       Status = "missing"
	StatusInvalid       Status = "invalid"
	StatusSetupRequired Status = "setup_required"
	StatusWaived        Status = "waived"
	StatusUnsupported   Status = "unsupported"
	StatusRevoked       Status = "revoked"
)

type Requirement struct {
	ToolchainKey     string      `json:"toolchain_key"`
	RequiredFor      RequiredFor `json:"required_for"`
	RequiredForMerge bool        `json:"required_for_merge"`
	Status           Status      `json:"status"`
	Executable       string      `json:"executable,omitempty"`
	DetectedPath     string      `json:"detected_path,omitempty"`
	Message          string      `json:"message"`
}

type Report struct {
	EnvironmentID string        `json:"environment_id"`
	Requirements  []Requirement `json:"requirements"`
}

type Options struct {
	IncludeCodex bool
	LookupPath   func(file string) (string, error)
	LookupEnv    func(key string) (string, bool)
	FileExists   func(path string) bool
}

func RunDoctor(ctx context.Context, env platform.ExecutionEnvironment, opts Options) Report {
	_ = ctx
	lookup := opts.LookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	lookupEnv := opts.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	fileExists := opts.FileExists
	if fileExists == nil {
		fileExists = regularFileExists
	}

	report := Report{EnvironmentID: env.ID}
	report.Requirements = append(report.Requirements, checkExecutable(lookup, "git", executableForGit(env), RequiredForImplementation, true))
	report.Requirements = append(report.Requirements, checkExecutable(lookup, string(env.Shell), executableForShell(env), RequiredForVerification, true))

	if env.SandboxProfile == platform.SandboxLinuxBubblewrap {
		report.Requirements = append(report.Requirements, checkExecutable(lookup, "bubblewrap", "bwrap", RequiredForImplementation, false))
	}
	if opts.IncludeCodex && env.CodexAdapter != platform.CodexAdapterNone {
		req := checkExecutable(lookup, "codex", "codex", RequiredForImplementation, true)
		if req.Status == StatusMissing {
			req.Status = StatusSetupRequired
			req.Message = "Codex CLI is required only for real Codex adapter runs"
		}
		report.Requirements = append(report.Requirements, req)
		report.Requirements = append(report.Requirements, checkCodexAuth(env, lookupEnv, fileExists))
	}
	return report
}

func (r Report) HasRequiredMergeFailure() bool {
	for _, req := range r.Requirements {
		if req.RequiredForMerge && (req.Status == StatusMissing || req.Status == StatusInvalid || req.Status == StatusSetupRequired || req.Status == StatusUnsupported) {
			return true
		}
	}
	return false
}

func checkExecutable(lookup func(string) (string, error), key string, executable string, requiredFor RequiredFor, requiredForMerge bool) Requirement {
	req := Requirement{
		ToolchainKey:     key,
		RequiredFor:      requiredFor,
		RequiredForMerge: requiredForMerge,
		Executable:       executable,
	}
	if executable == "" || executable == "none" {
		req.Status = StatusUnsupported
		req.Message = fmt.Sprintf("%s has no executable for this environment", key)
		return req
	}
	path, err := lookup(executable)
	if err != nil {
		req.Status = StatusMissing
		req.Message = fmt.Sprintf("%s executable not found", executable)
		return req
	}
	req.Status = StatusDetected
	req.DetectedPath = path
	req.Message = fmt.Sprintf("%s detected", executable)
	return req
}

func executableForGit(env platform.ExecutionEnvironment) string {
	if env.GitProvider == platform.GitProviderNone {
		return "none"
	}
	return "git"
}

func executableForShell(env platform.ExecutionEnvironment) string {
	switch env.Shell {
	case platform.ShellPowerShell:
		return "powershell"
	case platform.ShellCmd:
		return "cmd"
	case platform.ShellBash:
		return "bash"
	case platform.ShellSh:
		return "sh"
	default:
		return "none"
	}
}

func checkCodexAuth(env platform.ExecutionEnvironment, lookupEnv func(string) (string, bool), fileExists func(string) bool) Requirement {
	req := Requirement{
		ToolchainKey:     "codex-auth",
		RequiredFor:      RequiredForImplementation,
		RequiredForMerge: true,
		Executable:       "auth.json",
	}
	codexHome, source := codexHomeForEnvironment(env, lookupEnv)
	if strings.TrimSpace(codexHome) == "" {
		req.Status = StatusSetupRequired
		req.Message = "CODEX_HOME could not be determined for this environment"
		return req
	}
	req.DetectedPath = codexHome
	authPath := joinCodexHome(env.OSFamily, codexHome, "auth.json")
	if !fileExists(authPath) {
		req.Status = StatusSetupRequired
		req.Message = fmt.Sprintf("Codex auth is not detected in %s for this environment", source)
		return req
	}
	req.Status = StatusDetected
	req.Message = fmt.Sprintf("Codex auth detected in %s for this environment", source)
	return req
}

func codexHomeForEnvironment(env platform.ExecutionEnvironment, lookupEnv func(string) (string, bool)) (string, string) {
	if value, ok := lookupEnv("CODEX_HOME"); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), "CODEX_HOME"
	}
	switch env.OSFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		if userProfile, ok := lookupEnv("USERPROFILE"); ok && strings.TrimSpace(userProfile) != "" {
			return joinCodexHome(env.OSFamily, strings.TrimSpace(userProfile), ".codex"), "USERPROFILE"
		}
		drive, driveOK := lookupEnv("HOMEDRIVE")
		path, pathOK := lookupEnv("HOMEPATH")
		if driveOK && pathOK && strings.TrimSpace(drive+path) != "" {
			return joinCodexHome(env.OSFamily, strings.TrimSpace(drive+path), ".codex"), "HOMEDRIVE/HOMEPATH"
		}
	default:
		if home, ok := lookupEnv("HOME"); ok && strings.TrimSpace(home) != "" {
			return joinCodexHome(env.OSFamily, strings.TrimSpace(home), ".codex"), "HOME"
		}
	}
	return "", ""
}

func joinCodexHome(osFamily platform.OSFamily, root string, elem string) string {
	root = strings.TrimRight(strings.TrimSpace(root), `/\`)
	switch osFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		return root + `\` + elem
	default:
		return root + "/" + elem
	}
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
