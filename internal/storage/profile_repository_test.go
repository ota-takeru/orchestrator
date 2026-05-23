package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestConfigureFakeRunProfileWindowsPrimary(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	profile, err := db.ConfigureFakeRunProfile(ctx, "PROJECT-001", platform.PlatformModeWindowsPrimary, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Mode != platform.PlatformModeWindowsPrimary || profile.PrimaryEnvironmentID != "windows-main" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	var primaryCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_environments WHERE project_id = 'PROJECT-001' AND role = 'primary'").Scan(&primaryCount); err != nil {
		t.Fatal(err)
	}
	if primaryCount != 1 {
		t.Fatalf("primary count = %d", primaryCount)
	}
}

func TestConfigureFakeRunProfileHybridCreatesSidecar(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	profile, err := db.ConfigureFakeRunProfile(ctx, "PROJECT-001", platform.PlatformModeHybrid, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.RequiredVerificationEnvironments) != 1 || len(profile.OptionalVerificationEnvironments) != 1 {
		t.Fatalf("unexpected profile verification envs: %#v", profile)
	}
	var envCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_environments WHERE project_id = 'PROJECT-001'").Scan(&envCount); err != nil {
		t.Fatal(err)
	}
	if envCount != 2 {
		t.Fatalf("env count = %d", envCount)
	}
}

func TestListRunProfiles(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.ConfigureFakeRunProfile(ctx, "PROJECT-001", platform.PlatformModeWSLPrimary, "/repo"); err != nil {
		t.Fatal(err)
	}
	profiles, err := db.ListRunProfiles(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Mode != platform.PlatformModeWSLPrimary {
		t.Fatalf("unexpected profiles: %#v", profiles)
	}
}
