package platform

import "testing"

func TestProjectRequiresExactlyOnePrimaryEnvironment(t *testing.T) {
	env := DetectHostEnvironment("/repo")
	if err := ValidatePrimaryEnvironment([]ExecutionEnvironment{env}); err != nil {
		t.Fatalf("one primary environment should be valid: %v", err)
	}

	sidecar := env
	sidecar.ID = "linux-sidecar"
	sidecar.Role = RoleSidecar
	if err := ValidatePrimaryEnvironment([]ExecutionEnvironment{env, sidecar}); err != nil {
		t.Fatalf("one primary plus sidecar should be valid: %v", err)
	}

	if err := ValidatePrimaryEnvironment([]ExecutionEnvironment{sidecar}); err == nil {
		t.Fatal("expected missing primary environment to fail")
	}

	otherPrimary := env
	otherPrimary.ID = "other-primary"
	if err := ValidatePrimaryEnvironment([]ExecutionEnvironment{env, otherPrimary}); err == nil {
		t.Fatal("expected multiple primary environments to fail")
	}
}

func TestDetectHostEnvironmentDetectsWSL(t *testing.T) {
	env := detectHostEnvironment("linux", "/repo", "6.6.114.1-microsoft-standard-WSL2")
	if env.ID != "wsl-main" || env.OSFamily != OSFamilyWSL || env.CodexAdapter != CodexAdapterWSL {
		t.Fatalf("env = %#v", env)
	}
}

func TestDetectHostEnvironmentKeepsPlainLinux(t *testing.T) {
	env := detectHostEnvironment("linux", "/repo", "6.8.0-generic")
	if env.ID != "linux-main" || env.OSFamily != OSFamilyLinux || env.CodexAdapter != CodexAdapterLinux {
		t.Fatalf("env = %#v", env)
	}
}

func TestEnumValidationRejectsUnknownValues(t *testing.T) {
	if ValidOSFamily("plan9") {
		t.Fatal("unexpected OS family accepted")
	}
	if ValidPlatformMode("windows-primary") {
		t.Fatal("display platform mode must not be accepted as storage value")
	}
	if !ValidPlatformMode(PlatformModeWindowsPrimary) {
		t.Fatal("canonical platform mode rejected")
	}
}
