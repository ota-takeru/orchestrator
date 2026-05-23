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
