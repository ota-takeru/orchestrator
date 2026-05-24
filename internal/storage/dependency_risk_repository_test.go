package storage

import (
	"context"
	"testing"
)

func TestRecordDependencyRisk(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	record, err := db.RecordDependencyRisk(ctx, DependencyRiskInput{
		ProjectID:          "PROJECT-001",
		Name:               "github.com/example/library",
		PackageManager:     "go",
		DependencyType:     "production",
		IntroducedByTaskID: "TASK-001",
		Reason:             "Required for parsing canonical manifests",
		ApprovedBy:         "human",
		Risk:               "medium",
		LockfileChanged:    true,
		LifecycleScripts:   "none_detected",
		CurrentVersion:     "v1.2.3",
		ApprovedScope:      "task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID == "" || record.IntroducedByTaskID != "TASK-001" || !record.LockfileChanged {
		t.Fatalf("record = %#v", record)
	}

	records, err := db.ListDependencyRisks(ctx, DependencyRiskListFilter{
		ProjectID:      "PROJECT-001",
		PackageManager: "go",
		DependencyType: "production",
		Risk:           "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "github.com/example/library" || records[0].ApprovedScope != "task" {
		t.Fatalf("records = %#v", records)
	}

	var eventCount int
	if err := db.SQL().QueryRowContext(ctx, `
SELECT COUNT(*) FROM workflow_events
WHERE project_id = 'PROJECT-001' AND event_type = 'dependency_risk_recorded'`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d, want 1", eventCount)
	}
}

func TestRecordDependencyRiskValidatesLedgerEnums(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")

	_, err := db.RecordDependencyRisk(ctx, DependencyRiskInput{
		ProjectID:        "PROJECT-001",
		Name:             "zod",
		PackageManager:   "npm",
		DependencyType:   "blocks_merge",
		Reason:           "UI validation",
		Risk:             "medium",
		LifecycleScripts: "unknown",
		ApprovedScope:    "project",
	})
	if err == nil {
		t.Fatal("expected invalid ledger dependency type to fail")
	}
}
