package contextbuilder

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DocSet struct {
	IndexPath string   `json:"index_path"`
	Docs      []string `json:"docs"`
}

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func LoadCanonicalDocs(repoRoot string) (DocSet, error) {
	indexPath := filepath.Join(repoRoot, "docs", "index.md")
	file, err := os.Open(indexPath)
	if err != nil {
		return DocSet{}, err
	}
	defer file.Close()

	var docs []string
	inCanonicalSection := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			inCanonicalSection = strings.TrimSpace(line) == "## Canonical Implementation Docs"
			continue
		}
		if !inCanonicalSection || !strings.HasPrefix(strings.TrimSpace(line), "- ") {
			continue
		}
		matches := markdownLinkPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		docPath := filepath.Clean(filepath.Join("docs", matches[1]))
		if err := validateDocPath(docPath); err != nil {
			return DocSet{}, err
		}
		docs = append(docs, docPath)
	}
	if err := scanner.Err(); err != nil {
		return DocSet{}, err
	}
	if len(docs) == 0 {
		return DocSet{}, fmt.Errorf("canonical docs section is empty")
	}
	return DocSet{IndexPath: filepath.ToSlash(filepath.Join("docs", "index.md")), Docs: docs}, nil
}

func validateDocPath(path string) error {
	path = filepath.ToSlash(path)
	if strings.Contains(path, "..") {
		return fmt.Errorf("canonical doc path escapes docs root: %s", path)
	}
	if strings.HasPrefix(path, "docs/archive/") {
		return fmt.Errorf("archive doc must not be canonical: %s", path)
	}
	if strings.Contains(path, ".env") {
		return fmt.Errorf("secret-like path must not be canonical: %s", path)
	}
	if !strings.HasPrefix(path, "docs/") || !strings.HasSuffix(path, ".md") {
		return fmt.Errorf("canonical doc must be docs/*.md: %s", path)
	}
	return nil
}

func (d DocSet) Contains(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, doc := range d.Docs {
		if filepath.ToSlash(doc) == normalized {
			return true
		}
	}
	return false
}
