package preflight

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveProjectRoot(t *testing.T) {
	root := newGitRepo(t)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveProjectRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestPreflightDetectsCaseCollision(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, filepath.Join(root, "README.md"), "a")
	writeFile(t, filepath.Join(root, "Readme.md"), "b")

	report, err := Run(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, finding := range report.Findings {
		if finding.ID == "case_sensitive_filename_collision" {
			found = true
			if finding.Severity != SeverityBlock {
				t.Fatalf("collision severity = %s, want block", finding.Severity)
			}
		}
	}
	if !found {
		t.Fatal("case collision finding not found")
	}
}

func TestPreflightIgnoresDependencySymlinks(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "node_modules/\n.devagent-worktrees/\norchestrator-data/\n.env.*\n")
	writeFile(t, filepath.Join(root, ".gitattributes"), "* text=auto\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "target.txt"), "dependency")
	if err := os.Symlink("target.txt", filepath.Join(root, "node_modules", "pkg", "link.txt")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.ID == "symlink_support" && finding.Severity != SeverityPass {
			t.Fatalf("dependency symlink should be ignored: %#v", finding)
		}
	}
}

func TestInitProjectCreatesConceptAndPolicy(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".devagent-worktrees/\norchestrator-data/\n.env.*\n")
	writeFile(t, filepath.Join(root, ".gitattributes"), "* text=auto\n")

	result, err := InitProject(context.Background(), root, "作りたいアプリ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.ConceptPath); err != nil {
		t.Fatalf("concept not created: %v", err)
	}
	if _, err := os.Stat(result.PolicyPath); err != nil {
		t.Fatalf("policy not created: %v", err)
	}
	if len(result.CreatedPaths) != 11 {
		t.Fatalf("created paths = %d, want 11", len(result.CreatedPaths))
	}
	var schemaPass bool
	for _, finding := range result.PreflightReport.Findings {
		if finding.ID == "schema_registry" && finding.Severity == SeverityPass {
			schemaPass = true
		}
	}
	if !schemaPass {
		t.Fatalf("schema registry pass finding not found: %#v", result.PreflightReport.Findings)
	}
}

func TestRepairSchemasRestoresTamperedRegistry(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".devagent-worktrees/\norchestrator-data/\n.env.*\n")
	writeFile(t, filepath.Join(root, ".gitattributes"), "* text=auto\n")
	if _, err := InitProject(context.Background(), root, "作りたいアプリ"); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, ".devagent", "schemas", "semantic-behavior-diff.v1.schema.json")
	writeFile(t, schemaPath, `{"tampered":true}`)

	report, err := Run(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasBlocks() {
		t.Fatalf("expected tampered schema to block: %#v", report.Findings)
	}
	repaired, err := RepairSchemas(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.PreflightReport.HasBlocks() {
		t.Fatalf("repair report = %#v", repaired.PreflightReport.Findings)
	}
	if len(repaired.SchemaInstall.UpdatedPaths) == 0 {
		t.Fatalf("expected schema update: %#v", repaired.SchemaInstall)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	return root
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
