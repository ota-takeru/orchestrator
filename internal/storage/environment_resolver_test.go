package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestResolveCanonicalGitEnvironmentUsesActiveRunProfile(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.ConfigureFakeRunProfile(ctx, "PROJECT-001", platform.PlatformModeHybrid, "/repo"); err != nil {
		t.Fatal(err)
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "windows-main" {
		t.Fatalf("canonical git environment = %s", env.ID)
	}
}

func TestResolveCanonicalGitEnvironmentFallsBackToPrimary(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	env, err := db.ResolveCanonicalGitEnvironment(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "linux-main" {
		t.Fatalf("canonical git environment = %s", env.ID)
	}
}
