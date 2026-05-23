package storage

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func TestSaveGateResultsPersistsAndProjectsReportOnlyInbox(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertRunForGate(t, db, "PROJECT-001", "RUN-001")

	results := []decisions.GateResult{
		{
			Status:   decisions.GateReportOnly,
			Severity: decisions.SeverityLow,
			Detector: "optional_verification_failed",
			Evidence: map[string]any{"command_id": "smoke"},
		},
	}
	if err := db.SaveGateResults(ctx, "PROJECT-001", nil, "RUN-001", results); err != nil {
		t.Fatal(err)
	}
	var gateCount, reportInboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM gate_results").Scan(&gateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE item_type = 'report'").Scan(&reportInboxCount); err != nil {
		t.Fatal(err)
	}
	if gateCount != 1 || reportInboxCount != 1 {
		t.Fatalf("gate=%d report_inbox=%d", gateCount, reportInboxCount)
	}
}

func TestSaveGateResultsDoesNotProjectPassInbox(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertEnvironment(t, db.SQL(), "linux-main", "PROJECT-001", "primary")
	insertRunForGate(t, db, "PROJECT-001", "RUN-001")

	results := []decisions.GateResult{
		{
			Status:   decisions.GatePass,
			Severity: decisions.SeverityLow,
			Detector: "verification_passed",
			Evidence: map[string]any{"run_id": "RUN-001"},
		},
	}
	if err := db.SaveGateResults(ctx, "PROJECT-001", nil, "RUN-001", results); err != nil {
		t.Fatal(err)
	}
	var inboxCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items").Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 0 {
		t.Fatalf("inbox count = %d", inboxCount)
	}
}

func insertRunForGate(t *testing.T, db *DB, projectID string, runID string) {
	t.Helper()
	_, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO runs(
  id, project_id, run_type, status, attempt_no, base_commit,
  created_at, updated_at, started_at, completed_at
) VALUES (?, ?, 'verification', 'succeeded', 1, 'BASE', ?, ?, ?, ?)`,
		runID, projectID, now(), now(), now(), now(),
	)
	if err != nil {
		t.Fatal(err)
	}
}
