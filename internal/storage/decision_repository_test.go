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
