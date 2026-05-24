package schemas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallAndValidateSchemaRegistry(t *testing.T) {
	root := t.TempDir()
	result, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedPaths) != 6 {
		t.Fatalf("created paths = %#v", result.CreatedPaths)
	}
	validation := ValidateInstalled(root)
	if !validation.Valid {
		t.Fatalf("validation failed: %#v", validation.Findings)
	}
}

func TestValidateInstalledDetectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, ".devagent", "schemas", "task-yaml.v2.schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	validation := ValidateInstalled(root)
	if validation.Valid || len(validation.Findings) == 0 {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestValidateCodexFinalMessage(t *testing.T) {
	if err := ValidateCodexFinalMessage(`{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed"}]}`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCodexFinalMessage(`{"status":"ok","summary":"done"}`); err == nil {
		t.Fatal("expected invalid codex final status to fail")
	}
	if err := ValidateCodexFinalMessage(`not json`); err == nil {
		t.Fatal("expected non-json final message to fail")
	}
}

func TestValidateSemanticBehaviorDiff(t *testing.T) {
	if err := ValidateSemanticBehaviorDiff(`[{"category":"user_visible","summary":"UI changed","confidence":"high","evidence":[{"file":"ui/src/App.tsx","change_type":"modified","source":"git_diff","generated":false}]}]`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSemanticBehaviorDiff(`[{"category":"unknown","summary":"bad","confidence":"high","evidence":[]}]`); err == nil {
		t.Fatal("expected invalid category to fail")
	}
	if err := ValidateSemanticBehaviorDiff(`[]`); err == nil {
		t.Fatal("expected empty diff report to fail")
	}
}

func TestValidateDependencyRiskLedgerEntry(t *testing.T) {
	valid := `{
	  "id":"DEPRISK-ABC123",
	  "project_id":"PROJECT-001",
	  "name":"zod",
	  "package_manager":"npm",
	  "dependency_type":"production",
	  "reason":"Runtime schema validation",
	  "risk":"medium",
	  "lockfile_changed":true,
	  "lifecycle_scripts":"unknown",
	  "approved_scope":"project",
	  "created_at":"2026-05-24T00:00:00Z",
	  "updated_at":"2026-05-24T00:00:00Z"
	}`
	if err := ValidateDependencyRiskLedgerEntry(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDependencyRiskLedgerEntry(`{"id":"DEPRISK-ABC123","project_id":"PROJECT-001","name":"zod","package_manager":"npm","dependency_type":"blocks_merge","reason":"bad","risk":"medium","lockfile_changed":true,"lifecycle_scripts":"unknown","approved_scope":"project","created_at":"now","updated_at":"now"}`); err == nil {
		t.Fatal("expected invalid ledger dependency type to fail")
	}
	if err := ValidateDependencyRiskLedgerEntry(`not json`); err == nil {
		t.Fatal("expected non-json ledger entry to fail")
	}
}

func TestValidateHumanInboxSnapshot(t *testing.T) {
	valid := `{
	  "project_id":"PROJECT-001",
	  "generated_at":"2026-05-24T00:00:00Z",
	  "counts":{
	    "open_inbox_items":1,
	    "running_tasks":0,
	    "waiting_for_human_tasks":1,
	    "blocked_tasks":0,
	    "queued_requests":0,
	    "open_decisions":1,
	    "running_workers":0,
	    "open_merge_queue":0
	  },
	  "open_inbox_items":[{"id":"INBOX-001"}],
	  "recommended_next_commands":["devos inbox --status open --json"]
	}`
	if err := ValidateHumanInboxSnapshot(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"","generated_at":"2026-05-24T00:00:00Z","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0},"open_inbox_items":[]}`); err == nil {
		t.Fatal("expected empty project id to fail")
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"PROJECT-001","generated_at":"bad","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0},"open_inbox_items":[]}`); err == nil {
		t.Fatal("expected invalid generated_at to fail")
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"PROJECT-001","generated_at":"2026-05-24T00:00:00Z","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0},"open_inbox_items":[{"id":"INBOX-001"}]}`); err == nil {
		t.Fatal("expected item/count mismatch to fail")
	}
}
