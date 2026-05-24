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

func TestSaveToolchainReportResolvesDetectedSetupCard(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	missing := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
			{
				ToolchainKey:     "bubblewrap",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: false,
				Status:           toolchains.StatusMissing,
				Message:          "bwrap executable not found",
			},
		},
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", missing); err != nil {
		t.Fatal(err)
	}
	detected := missing
	detected.Requirements[0].Status = toolchains.StatusDetected
	detected.Requirements[0].Message = "bwrap detected"
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", detected); err != nil {
		t.Fatal(err)
	}
	var openCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'toolchain_setup' AND status = 'open'").Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if openCount != 0 {
		t.Fatalf("open setup card count = %d", openCount)
	}
}

func TestToolchainSetupInstructions(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	report := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
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
	var inboxID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM inbox_items WHERE item_type = 'toolchain_setup'").Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	instructions, err := db.ToolchainSetupInstructions(ctx, "PROJECT-001", inboxID)
	if err != nil {
		t.Fatal(err)
	}
	if instructions.ToolchainKey != "bubblewrap" || len(instructions.Instructions) == 0 || instructions.RerunCommand == "" {
		t.Fatalf("instructions = %#v", instructions)
	}
}

func TestWaiveToolchainRequirementRecordsDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	report := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
			{
				ToolchainKey:     "codex-auth",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: true,
				Status:           toolchains.StatusSetupRequired,
				Message:          "Codex auth is not detected",
			},
		},
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", report); err != nil {
		t.Fatal(err)
	}
	var inboxID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM inbox_items WHERE item_type = 'toolchain_setup'").Scan(&inboxID); err != nil {
		t.Fatal(err)
	}

	waiver, err := db.WaiveToolchainRequirement(ctx, ToolchainWaiverInput{
		ProjectID:     "PROJECT-001",
		InboxID:       inboxID,
		Reason:        "temporary local validation only",
		Scope:         "TASK-001",
		Expiry:        "2026-06-01T00:00:00Z",
		AllowedEffect: "allow_non_merge_without_toolchain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiver.Status != "waived" || waiver.AllowedEffect != "allow_non_merge_without_toolchain" || waiver.RequirementKey != "codex-auth" {
		t.Fatalf("waiver = %#v", waiver)
	}
	var reqStatus, inboxStatus, decisionStatus, selectedOption string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM toolchain_requirements WHERE id = ?", waiver.RequirementID).Scan(&reqStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM inbox_items WHERE id = ?", inboxID).Scan(&inboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT status, selected_option FROM decisions WHERE id = ?", waiver.DecisionID).Scan(&decisionStatus, &selectedOption); err != nil {
		t.Fatal(err)
	}
	if reqStatus != "waived" || inboxStatus != "resolved" || decisionStatus != "approved" || selectedOption != "allow_non_merge_without_toolchain" {
		t.Fatalf("req=%s inbox=%s decision=%s option=%s", reqStatus, inboxStatus, decisionStatus, selectedOption)
	}
}
