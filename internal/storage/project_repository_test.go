package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/preflight"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

func TestSaveProjectInitPersistsProjectEnvironmentAndEvidence(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	env := platform.DetectHostEnvironment("/repo")
	env.ID = "linux-main"
	report := preflight.Report{ProjectRoot: "/repo", Environment: env}
	toolchainReport := toolchains.Report{EnvironmentID: "linux-main"}

	record, err := db.SaveProjectInit(ctx, ProjectInitInput{
		Name:            "Project",
		RootPath:        "/repo",
		Environment:     env,
		PreflightReport: report,
		ToolchainReport: &toolchainReport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID == "" || record.PrimaryEnvironmentID != "linux-main" || !record.Created {
		t.Fatalf("unexpected record: %#v", record)
	}

	var projectCount, envCount, eventCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projectCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_environments").Scan(&envCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_events").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if projectCount != 1 || envCount != 1 || eventCount != 2 {
		t.Fatalf("counts project=%d env=%d event=%d", projectCount, envCount, eventCount)
	}
}

func TestSaveProjectInitIsIdempotentForProjectAndEnvironment(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	env := platform.DetectHostEnvironment("/repo")
	report := preflight.Report{ProjectRoot: "/repo", Environment: env}

	first, err := db.SaveProjectInit(ctx, ProjectInitInput{RootPath: "/repo", Environment: env, PreflightReport: report})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.SaveProjectInit(ctx, ProjectInitInput{RootPath: "/repo", Environment: env, PreflightReport: report})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("project ids differ: %s != %s", first.ID, second.ID)
	}
	if !first.Created || second.Created {
		t.Fatalf("created flags first=%v second=%v", first.Created, second.Created)
	}

	var projectCount, envCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projectCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_environments").Scan(&envCount); err != nil {
		t.Fatal(err)
	}
	if projectCount != 1 || envCount != 1 {
		t.Fatalf("counts project=%d env=%d", projectCount, envCount)
	}
}
