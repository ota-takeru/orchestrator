package artifactgen

import (
	"strings"
	"testing"
)

func TestInitialArtifactsDescribeUserProductNotOrchestrator(t *testing.T) {
	artifacts := BuildInitialArtifacts(t.TempDir(), "A tiny habit tracker for students.", true)
	if len(artifacts) != 4 {
		t.Fatalf("artifact count = %d", len(artifacts))
	}
	for _, artifact := range artifacts {
		content := strings.ToLower(string(artifact.Content))
		for _, disallowed := range []string{"devos", "orchestrator", "human inbox", "implemented in go", "react", "typescript", "sqlite"} {
			if strings.Contains(content, disallowed) {
				t.Fatalf("%s leaked into %s:\n%s", disallowed, artifact.Path, string(artifact.Content))
			}
		}
	}
	if !strings.Contains(string(artifacts[0].Content), "A tiny habit tracker for students.") {
		t.Fatalf("concept missing from PRD:\n%s", string(artifacts[0].Content))
	}
}
