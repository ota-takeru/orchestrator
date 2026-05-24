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

func TestApproveInboxItemDispatchesHumanApprovalSource(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	seedApprovalTaskEvidence(t, db, ctx)
	insertOpenHumanApprovalWithInbox(t, db, "PROJECT-001", "APPROVAL-FINAL", "TASK-001", ApprovalFinalReview, approvalEvidenceJSON(t, db, ctx))

	result, err := db.ApproveInboxItem(ctx, InboxApprovalInput{
		ProjectID: "PROJECT-001",
		InboxID:   "INBOX-APPROVAL-FINAL",
		Notes:     "approved via inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceType != "human_approval" || result.HumanApproval == nil {
		t.Fatalf("inbox approval = %#v", result)
	}
	if result.HumanApproval.ID != "APPROVAL-FINAL" || result.HumanApproval.TaskStatus != "ready_for_human_review" {
		t.Fatalf("human approval = %#v", result.HumanApproval)
	}
}

func TestInboxIsProjectionNotSourceOfTruth(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-MISSING-DECISION', 'PROJECT-001', 'human_decision', 'open', 'decision', 'DEC-MISSING',
  'decision:DEC-MISSING', 80, 'Missing decision', 'Projection without source', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveInboxItem(ctx, InboxApprovalInput{
		ProjectID: "PROJECT-001",
		InboxID:   "INBOX-MISSING-DECISION",
		Option:    "A",
	}); err == nil {
		t.Fatal("expected inbox projection without decision source to fail")
	}
	var status string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM inbox_items WHERE id = 'INBOX-MISSING-DECISION'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "open" {
		t.Fatalf("projection status = %s", status)
	}
}

func TestHardBlockCannotBeApprovedThrough(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-HARD-BLOCK', 'PROJECT-001', 'hard_block', 'open', 'gate_result', 'GATE-HARD',
  'gate:GATE-HARD', 100, 'Hard block', 'Cannot approve through inbox', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApproveInboxItem(ctx, InboxApprovalInput{
		ProjectID: "PROJECT-001",
		InboxID:   "INBOX-HARD-BLOCK",
	}); err == nil {
		t.Fatal("expected hard block approve to fail")
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
