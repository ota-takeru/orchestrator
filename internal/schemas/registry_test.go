package schemas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndValidateSchemaRegistry(t *testing.T) {
	root := t.TempDir()
	result, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedPaths) != 9 {
		t.Fatalf("created paths = %#v", result.CreatedPaths)
	}
	validation := ValidateInstalled(root)
	if !validation.Valid {
		t.Fatalf("validation failed: %#v", validation.Findings)
	}
}

func TestValidateToolchainReport(t *testing.T) {
	valid := `{
	  "environment_id":"wsl-main",
	  "requirements":[
	    {
	      "toolchain_key":"corepack",
	      "required_for":"verification",
	      "required_for_merge":true,
	      "status":"detected",
	      "executable":"corepack",
	      "detected_path":"/usr/bin/corepack",
	      "message":"corepack detected"
	    }
	  ]
	}`
	if err := ValidateToolchainReport(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolchainReport(`{"environment_id":"wsl-main","requirements":[]}`); err == nil {
		t.Fatal("expected empty requirements to fail")
	}
	if err := ValidateToolchainReport(`{"environment_id":"wsl-main","requirements":[{"toolchain_key":"node","required_for":"unknown","required_for_merge":true,"status":"detected","message":"ok"}]}`); err == nil {
		t.Fatal("expected invalid required_for to fail")
	}
	if err := ValidateToolchainReport(`{"environment_id":"wsl-main","requirements":[{"toolchain_key":"node","required_for":"verification","required_for_merge":true,"status":"detected","message":"ok"},{"toolchain_key":"node","required_for":"verification","required_for_merge":true,"status":"detected","message":"ok"}]}`); err == nil {
		t.Fatal("expected duplicate requirement to fail")
	}
}

func TestValidateCodexRuntimeReadiness(t *testing.T) {
	valid := `{
	  "host_goos":"linux",
	  "items":[
	    {
	      "environment_id":"wsl-main",
	      "os_family":"wsl",
	      "project_root":"/repo",
	      "codex_adapter":"codex-wsl",
	      "sandbox_profile":"linux-bubblewrap",
	      "expected_host_runtime":"linux",
	      "current_runtime_usable":true,
	      "classification":"ready",
	      "argv":["exec","--json"]
	    }
	  ]
	}`
	if err := ValidateCodexRuntimeReadiness(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCodexRuntimeReadiness(`{"host_goos":"linux","items":[{"environment_id":"wsl-main","os_family":"wsl","project_root":"/repo","codex_adapter":"codex-wsl","sandbox_profile":"linux-bubblewrap","expected_host_runtime":"linux","current_runtime_usable":false,"classification":"toolchain_required"}]}`); err == nil {
		t.Fatal("expected unusable item without blockers to fail")
	}
	if err := ValidateCodexRuntimeReadiness(`{"host_goos":"linux","items":[{"environment_id":"wsl-main","os_family":"wsl","project_root":"/repo","codex_adapter":"codex-wsl","sandbox_profile":"linux-bubblewrap","expected_host_runtime":"linux","current_runtime_usable":true,"classification":"ready","blockers":["codex missing"]}]}`); err == nil {
		t.Fatal("expected usable item with blockers to fail")
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

func TestRepositorySchemaRegistryMatchesEmbeddedDefinitions(t *testing.T) {
	root, err := testRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	validation := ValidateInstalled(root)
	if !validation.Valid {
		t.Fatalf("repository schema registry drifted from embedded definitions: %#v", validation.Findings)
	}
}

func TestValidateCodexFinalMessage(t *testing.T) {
	if err := ValidateCodexFinalMessage(`{"status":"succeeded","summary":"done","tests":[{"command":"go test ./...","status":"passed","notes":"ok"}],"blockers":[]}`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCodexFinalMessage(`{"status":"ok","summary":"done"}`); err == nil {
		t.Fatal("expected invalid codex final status to fail")
	}
	if err := ValidateCodexFinalMessage(`not json`); err == nil {
		t.Fatal("expected non-json final message to fail")
	}
}

func testRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", os.ErrNotExist
		}
		dir = next
	}
}

func TestCodexFinalMessageSchemaIsStrictForStructuredOutputs(t *testing.T) {
	schema := string(CodexFinalMessageSchema())
	for _, required := range []string{
		`"required": ["summary", "status", "tests", "blockers"]`,
		`"required": ["command", "status", "notes"]`,
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("codex final schema missing strict required set %s:\n%s", required, schema)
		}
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
	    "open_merge_queue":0,
	    "baseline_issues":1
	  },
	  "open_inbox_items":[{"id":"INBOX-001"}],
	  "recommended_next_commands":["devos inbox --status open --json"]
	}`
	if err := ValidateHumanInboxSnapshot(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"","generated_at":"2026-05-24T00:00:00Z","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0,"baseline_issues":0},"open_inbox_items":[]}`); err == nil {
		t.Fatal("expected empty project id to fail")
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"PROJECT-001","generated_at":"bad","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0,"baseline_issues":0},"open_inbox_items":[]}`); err == nil {
		t.Fatal("expected invalid generated_at to fail")
	}
	if err := ValidateHumanInboxSnapshot(`{"project_id":"PROJECT-001","generated_at":"2026-05-24T00:00:00Z","counts":{"open_inbox_items":0,"running_tasks":0,"waiting_for_human_tasks":0,"blocked_tasks":0,"queued_requests":0,"open_decisions":0,"running_workers":0,"open_merge_queue":0,"baseline_issues":0},"open_inbox_items":[{"id":"INBOX-001"}]}`); err == nil {
		t.Fatal("expected item/count mismatch to fail")
	}
}

func TestValidateGateResult(t *testing.T) {
	valid := `{
	  "status":"HUMAN_DECISION",
	  "severity":"high",
	  "detector":"required_verification_unclassified",
	  "human_action_type":"decision",
	  "evidence":{"run_id":"RUN-001"}
	}`
	if err := ValidateGateResult(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGateResult(`{"status":"OK","severity":"high","detector":"x","evidence":{"run_id":"RUN-001"}}`); err == nil {
		t.Fatal("expected invalid gate status to fail")
	}
	if err := ValidateGateResult(`{"status":"PASS","severity":"low","detector":"","evidence":{"run_id":"RUN-001"}}`); err == nil {
		t.Fatal("expected empty gate detector to fail")
	}
	if err := ValidateGateResult(`{"status":"PASS","severity":"low","detector":"verification_passed","evidence":[]}`); err == nil {
		t.Fatal("expected empty gate evidence array to fail")
	}
}
