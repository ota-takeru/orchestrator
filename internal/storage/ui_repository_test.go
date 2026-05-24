package storage

import (
	"context"
	"testing"
)

func TestLoadHumanInboxSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openMigratedTestDB(t)
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-RUN", "implementing")
	insertTask(t, db, "PROJECT-001", "TASK-WAIT", "needs_decision")
	now := now()
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, task_id, status, title, options_json, evidence_json, created_at, updated_at
) VALUES ('DEC-001', 'PROJECT-001', 'TASK-WAIT', 'open', 'Need decision', '[]', '{}', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, priority, title, body, created_at, updated_at
) VALUES (
  'INBOX-001', 'PROJECT-001', 'TASK-WAIT', 'human_decision', 'open', 'decision', 'DEC-001',
  'decision:DEC-001', 80, 'Need decision', 'Choose option', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO feature_requests(
  id, project_id, status, body, title, description, source, priority, created_at, updated_at
) VALUES ('FR-001', 'PROJECT-001', 'queued', 'Feature', 'Feature', 'Feature', 'human', 'medium', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
INSERT INTO memories(
  id, project_id, memory_type, key, value, scope, scope_id, source_type, source_id, created_at, updated_at
) VALUES (
  'MEM-BASELINE', 'PROJECT-001', 'baseline_issue', 'baseline_issue.GATE-001', '{}',
  'project', '', 'system', 'GATE-001', ?, ?
)`, now, now); err != nil {
		t.Fatal(err)
	}

	snapshot, err := db.LoadHumanInboxSnapshot(ctx, "PROJECT-001", 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.OpenInboxItems != 1 || snapshot.Counts.RunningTasks != 1 || snapshot.Counts.WaitingForHumanTasks != 1 || snapshot.Counts.QueuedRequests != 1 || snapshot.Counts.OpenDecisions != 1 || snapshot.Counts.BaselineIssues != 1 {
		t.Fatalf("snapshot counts = %#v", snapshot.Counts)
	}
	if len(snapshot.OpenInboxItems) != 1 || snapshot.OpenInboxItems[0].ID != "INBOX-001" {
		t.Fatalf("inbox items = %#v", snapshot.OpenInboxItems)
	}
	if len(snapshot.RecommendedNextCommands) == 0 {
		t.Fatalf("recommended commands missing: %#v", snapshot)
	}
}
