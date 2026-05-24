package storage

import (
	"context"
	"testing"
)

func TestCheckArtifactInvariantsPassesForApprovedContext(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	approveRequiredArtifacts(t, db, ctx, "PROJECT-001", t.TempDir(), "approved")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsBrokenApprovedReference(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "UPDATE artifacts SET approved_version_id = ? WHERE id = ?", second.VersionID, first.ArtifactID); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "approved_version_reference_invalid") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsReadyTaskWithoutTrustedArtifacts(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	insertTask(t, db, "PROJECT-001", "TASK-001", "ready")

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "ready_task_missing_trusted_artifacts") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCheckArtifactInvariantsDetectsApprovedWithNotesMissingNotes(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactRoadmap,
		Path:         ".devagent/roadmap.yaml",
		Content:      []byte("roadmap: []"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "UPDATE artifact_versions SET status = 'approved_with_notes', approval_notes = '' WHERE id = ?", record.VersionID); err != nil {
		t.Fatal(err)
	}

	violations, err := db.CheckArtifactInvariants(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariantViolation(violations, "approved_with_notes_missing_notes") {
		t.Fatalf("violations = %#v", violations)
	}
}

func hasInvariantViolation(violations []InvariantViolation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}
