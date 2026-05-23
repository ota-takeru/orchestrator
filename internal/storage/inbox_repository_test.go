package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

func TestListInboxItemsOrdersByPriority(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	report := toolchains.Report{
		EnvironmentID: "linux-main",
		Requirements: []toolchains.Requirement{
			{
				ToolchainKey:     "optional-tool",
				RequiredFor:      toolchains.RequiredForImplementation,
				RequiredForMerge: false,
				Status:           toolchains.StatusMissing,
				Message:          "optional missing",
			},
			{
				ToolchainKey:     "required-tool",
				RequiredFor:      toolchains.RequiredForVerification,
				RequiredForMerge: true,
				Status:           toolchains.StatusMissing,
				Message:          "required missing",
			},
		},
	}
	if err := db.SaveToolchainReport(ctx, "PROJECT-001", report); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListInboxItems(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("item count = %d", len(items))
	}
	if items[0].Priority < items[1].Priority {
		t.Fatalf("items not ordered by priority: %#v", items)
	}
}

func TestApproveInboxItemDispatchesDecisionSource(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertOpenDecisionWithInbox(t, db, "PROJECT-001", "DEC-001")

	result, err := db.ApproveInboxItem(ctx, InboxApprovalInput{
		ProjectID: "PROJECT-001",
		InboxID:   "INBOX-DEC-001",
		Option:    "A",
		Notes:     "approve via inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceType != "decision" || result.SourceID != "DEC-001" || result.Decision == nil {
		t.Fatalf("inbox approval = %#v", result)
	}
	if result.Decision.Status != "approved" || result.Decision.SelectedOption != "A" {
		t.Fatalf("decision = %#v", result.Decision)
	}
}

func TestValidateInboxStatusRejectsUnknown(t *testing.T) {
	if err := ValidateInboxStatus("open"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInboxStatus("waiting"); err == nil {
		t.Fatal("expected invalid status to fail")
	}
}
