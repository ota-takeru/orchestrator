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
	if len(result.CreatedPaths) != 2 {
		t.Fatalf("created paths = %d, want 2", len(result.CreatedPaths))
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
