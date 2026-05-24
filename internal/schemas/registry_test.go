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
	if len(result.CreatedPaths) != 4 {
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
