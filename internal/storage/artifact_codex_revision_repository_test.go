package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type artifactRevisionExecutor struct {
	request CodexExecRequest
	content string
}

func (f *artifactRevisionExecutor) ExecCodex(_ context.Context, request CodexExecRequest) (CodexExecResult, error) {
	f.request = request
	if err := os.WriteFile(filepath.Join(request.ProjectRoot, "prd.md"), []byte(f.content), 0o644); err != nil {
		return CodexExecResult{}, err
	}
	now := time.Now().UTC()
	return CodexExecResult{
		FinalMessage: `{"status":"succeeded","summary":"Updated the artifact from the requested instruction.","tests":[{"command":"artifact revision","status":"passed","notes":"content updated"}],"blockers":[]}`,
		ExitCode:     0,
		StartedAt:    now,
		CompletedAt:  now,
	}, nil
}

func TestReviseArtifactWithCodexCreatesProposedVersionFromInstruction(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := t.TempDir()
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", root)
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\nFirst draft."),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &artifactRevisionExecutor{content: "# PRD\n\nSecond draft with metrics."}

	result, err := db.ReviseArtifactWithCodex(ctx, ArtifactCodexRevisionInput{
		ProjectID:   "PROJECT-001",
		ArtifactID:  first.ArtifactID,
		Instruction: "Add measurable success metrics.",
		Executor:    executor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Classification != "succeeded" || result.Artifact.Version != 2 || result.Artifact.Status != "proposed" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(executor.request.Prompt, "Add measurable success metrics.") || !strings.Contains(executor.request.Prompt, "Edit only the file") {
		t.Fatalf("prompt did not include revision guard and instruction:\n%s", executor.request.Prompt)
	}
	content, err := os.ReadFile(filepath.Join(root, ".devagent", "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# PRD\n\nSecond draft with metrics." {
		t.Fatalf("project artifact content = %q", string(content))
	}
	var eventCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_events WHERE project_id = ? AND event_type = 'artifact_codex_revision_created'", "PROJECT-001").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d", eventCount)
	}
}

func TestReviseArtifactWithCodexRejectsNoChange(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	root := t.TempDir()
	insertProjectWithRoot(t, db, "PROJECT-001", root)
	insertEnvironmentWithRoot(t, db, "linux-main", "PROJECT-001", "primary", root)
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\nFirst draft."),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &artifactRevisionExecutor{content: "# PRD\n\nFirst draft."}

	if _, err := db.ReviseArtifactWithCodex(ctx, ArtifactCodexRevisionInput{
		ProjectID:   "PROJECT-001",
		ArtifactID:  first.ArtifactID,
		Instruction: "Improve it.",
		Executor:    executor,
	}); err == nil {
		t.Fatal("expected no-change revision to fail")
	}
}
