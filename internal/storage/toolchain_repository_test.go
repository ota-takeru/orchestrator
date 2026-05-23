package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

func TestSaveToolchainReportProjectsMissingToolchainToSetupInbox(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")

	report := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
			{
				ToolchainKey:     "git",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: true,
				Status:           toolchains.StatusDetected,
				Message:          "git detected",
			},
			{
				ToolchainKey:     "bubblewrap",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: false,
				Status:           toolchains.StatusMissing,
				Message:          "bwrap executable not found",
			},
		},
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", report); err != nil {
		t.Fatal(err)
	}

	var requirementCount, inboxCount, humanInputCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM toolchain_requirements").Scan(&requirementCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'toolchain_setup'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'human_input'").Scan(&humanInputCount); err != nil {
		t.Fatal(err)
	}
	if requirementCount != 2 || inboxCount != 1 || humanInputCount != 0 {
		t.Fatalf("requirements=%d toolchain_inbox=%d human_input=%d", requirementCount, inboxCount, humanInputCount)
	}
}

func TestSaveToolchainReportIsIdempotentForOpenSetupCard(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	report := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
			{
				ToolchainKey:     "codex",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: true,
				Status:           toolchains.StatusSetupRequired,
				Message:          "Codex CLI is required",
			},
		},
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", report); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", report); err != nil {
		t.Fatal(err)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'toolchain_setup'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count = %d, want 1", inboxCount)
	}
}
