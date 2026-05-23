package storage

import (
	"context"
	"testing"
)

func TestListDecisions(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES ('DEC-001', 'PROJECT-001', 'open', 'Choose behavior', '[]', '{}', ?, ?)`, now(), now()); err != nil {
		t.Fatal(err)
	}
	decisions, err := db.ListDecisions(ctx, "PROJECT-001", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].ID != "DEC-001" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestApproveDecisionResolvesDecisionAndInbox(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertOpenDecisionWithInbox(t, db, "PROJECT-001", "DEC-001")

	decision, err := db.ApproveDecision(ctx, DecisionApprovalInput{
		ProjectID:  "PROJECT-001",
		DecisionID: "DEC-001",
		Option:     "A",
		Notes:      "use recommended path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != "approved" || decision.SelectedOption != "A" || decision.ResolvedAt == "" {
		t.Fatalf("decision = %#v", decision)
	}

	var openInbox int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_items WHERE project_id = 'PROJECT-001' AND source_type = 'decision' AND source_id = 'DEC-001' AND status = 'open'").Scan(&openInbox); err != nil {
		t.Fatal(err)
	}
	if openInbox != 0 {
		t.Fatalf("open inbox count = %d", openInbox)
	}

	var events int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_events WHERE project_id = 'PROJECT-001' AND event_type = 'decision_approved'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("workflow event count = %d", events)
	}
}

func TestApproveDecisionRejectsResolvedDecision(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertOpenDecisionWithInbox(t, db, "PROJECT-001", "DEC-001")

	if _, err := db.ApproveDecision(ctx, DecisionApprovalInput{ProjectID: "PROJECT-001", DecisionID: "DEC-001", Option: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveDecision(ctx, DecisionApprovalInput{ProjectID: "PROJECT-001", DecisionID: "DEC-001", Option: "B"}); err == nil {
		t.Fatal("expected resolved decision approval to fail")
	}
}

func insertOpenDecisionWithInbox(t *testing.T, db *DB, projectID string, decisionID string) {
	t.Helper()
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO decisions(
  id, project_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES (?, ?, 'open', 'Choose behavior', '[]', '{}', ?, ?)`, decisionID, projectID, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  priority, title, body, created_at, updated_at
) VALUES (?, ?, 'human_decision', 'open', 'decision', ?, ?, 10, 'Choose behavior', 'Pick an option', ?, ?)`,
		"INBOX-"+decisionID, projectID, decisionID, "decision-"+decisionID, now(), now()); err != nil {
		t.Fatal(err)
	}
}
