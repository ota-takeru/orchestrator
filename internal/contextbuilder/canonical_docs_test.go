package contextbuilder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCanonicalDocsExcludesArchive(t *testing.T) {
	root := repoRoot(t)
	docs, err := LoadCanonicalDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	if docs.Contains("docs/archive/personal-dev-os-design.md") {
		t.Fatal("archive doc must not be canonical implementation context")
	}
	if !docs.Contains("docs/storage-schema.md") {
		t.Fatal("storage-schema.md should be canonical")
	}
}

func TestArchiveEntryInCanonicalSectionIsRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := "# Documentation Index\n\n## Canonical Implementation Docs\n\n- [bad](archive/bad.md)\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "index.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCanonicalDocs(root); err == nil {
		t.Fatal("expected archive canonical doc to be rejected")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
