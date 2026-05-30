package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitialProjectUnderstandingDefersArtifactsUntilPacketApproval(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)

	intake, err := db.CreateInitialProjectUnderstanding(ctx, "PROJECT-001", "Build a local-first planning app.")
	if err != nil {
		t.Fatal(err)
	}
	if intake.IntentItem.SourceType != "initial_concept" || intake.ApprovalPacket.Status != "open" || intake.ApprovalPacket.RiskLevel != "L2" {
		t.Fatalf("intake = %#v", intake)
	}
	var artifactsBefore int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM artifact_versions").Scan(&artifactsBefore); err != nil {
		t.Fatal(err)
	}
	if artifactsBefore != 0 {
		t.Fatalf("artifact versions before approval = %d", artifactsBefore)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_type = 'approval_packet' AND status = 'open'").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("approval packet inbox count = %d", inboxCount)
	}

	approved, err := db.ApproveApprovalPacket(ctx, ApprovalPacketApprovalInput{
		ProjectID: "PROJECT-001",
		PacketID:  intake.ApprovalPacket.ID,
		Option:    "approve_recommended",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.ApprovalPacket.Status != "approved" || len(approved.GeneratedArtifacts) != 4 {
		t.Fatalf("approved = %#v", approved)
	}
	if _, err := os.Stat(filepath.Join(root, ".devagent", "prd.md")); err != nil {
		t.Fatalf("prd artifact not written: %v", err)
	}
}

func TestFeatureRequestRiskGateCreatesAutoGoAndHardGatePackets(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	l1, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Add Today View")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartPlanning(ctx, PlanStartInput{ProjectID: "PROJECT-001", Concurrency: 1}); err != nil {
		t.Fatal(err)
	}
	autoGo, err := db.ConsolidatePlanning(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(autoGo.ProposedTasks) != 1 || autoGo.ProposedTasks[0].Status != "ready" {
		t.Fatalf("auto-go consolidation = %#v", autoGo)
	}
	packets, err := db.ListApprovalPackets(ctx, "PROJECT-001", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].SourceID != l1.FeatureRequest.ID || packets[0].Status != "approved" || packets[0].RiskLevel != "L1" {
		t.Fatalf("auto-go packets = %#v", packets)
	}

	l4, err := db.CreateFeatureRequest(ctx, "PROJECT-001", "Add DB schema migration and auth permission changes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartPlanning(ctx, PlanStartInput{ProjectID: "PROJECT-001", Concurrency: 1}); err != nil {
		t.Fatal(err)
	}
	hardGate, err := db.ConsolidatePlanning(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(hardGate.TaskGroups) != 0 || len(hardGate.ProposedTasks) != 0 {
		t.Fatalf("hard gate created ready work: %#v", hardGate)
	}
	openPackets, err := db.ListApprovalPackets(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(openPackets) != 1 || openPackets[0].SourceID != l4.FeatureRequest.ID || openPackets[0].RiskLevel != "L4" {
		t.Fatalf("hard gate packets = %#v", openPackets)
	}
	var hardBlockInbox int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_id = ? AND item_type = 'hard_block' AND status = 'open'", openPackets[0].ID).Scan(&hardBlockInbox); err != nil {
		t.Fatal(err)
	}
	if hardBlockInbox != 1 {
		t.Fatalf("hard block inbox = %d", hardBlockInbox)
	}
}

func TestApproveInboxItemDispatchesApprovalPacketSource(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	root := seedProjectRoot(t)
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	intake, err := db.CreateInitialProjectUnderstanding(ctx, "PROJECT-001", "Build a reviewable planning app.")
	if err != nil {
		t.Fatal(err)
	}
	items, err := db.ListInboxItems(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SourceID != intake.ApprovalPacket.ID {
		t.Fatalf("inbox items = %#v", items)
	}
	result, err := db.ApproveInboxItem(ctx, InboxApprovalInput{
		ProjectID: "PROJECT-001",
		InboxID:   items[0].ID,
		Option:    "approve_recommended",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ApprovalPacket == nil || result.ApprovalPacket.ApprovalPacket.Status != "approved" || len(result.ApprovalPacket.GeneratedArtifacts) != 4 {
		t.Fatalf("approval result = %#v", result)
	}
}
