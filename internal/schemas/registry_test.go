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
	if len(result.CreatedPaths) != 3 {
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
