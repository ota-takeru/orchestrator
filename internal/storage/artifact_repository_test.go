package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveArtifactVersionCreatesLatestVersion(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 1 || record.Status != "proposed" {
		t.Fatalf("unexpected record: %#v", record)
	}
	var latest string
	if err := db.SQL().QueryRowContext(ctx, "SELECT latest_version_id FROM artifacts WHERE id = ?", record.ArtifactID).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if latest != record.VersionID {
		t.Fatalf("latest version = %s", latest)
	}
}

func TestSaveArtifactVersionSameHashIsNoop(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	input := ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	}
	first, err := db.SaveArtifactVersion(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.SaveArtifactVersion(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.VersionID != second.VersionID {
		t.Fatalf("same content created new version: first=%#v second=%#v", first, second)
	}
}

func TestApproveArtifactVersionSetsApprovedVersion(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved", "")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" {
		t.Fatalf("approved status = %s", approved.Status)
	}
	var approvedVersion string
	if err := db.SQL().QueryRowContext(ctx, "SELECT approved_version_id FROM artifacts WHERE id = ?", record.ArtifactID).Scan(&approvedVersion); err != nil {
		t.Fatal(err)
	}
	if approvedVersion != record.VersionID {
		t.Fatalf("approved version = %s", approvedVersion)
	}
}

func TestApproveArtifactVersionIsIdempotentForSameReview(t *testing.T) {
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
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved_with_notes", "Keep order."); err != nil {
		t.Fatal(err)
	}
	var firstReviewedAt string
	if err := db.SQL().QueryRowContext(ctx, "SELECT reviewed_at FROM artifact_versions WHERE id = ?", record.VersionID).Scan(&firstReviewedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved_with_notes", "Keep order."); err != nil {
		t.Fatal(err)
	}

	var secondReviewedAt string
	var eventCount int
	if err := db.SQL().QueryRowContext(ctx, "SELECT reviewed_at FROM artifact_versions WHERE id = ?", record.VersionID).Scan(&secondReviewedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_events WHERE project_id = 'PROJECT-001' AND event_type = 'artifact_version_reviewed'").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if secondReviewedAt != firstReviewedAt || eventCount != 1 {
		t.Fatalf("reviewed_at first=%s second=%s events=%d", firstReviewedAt, secondReviewedAt, eventCount)
	}
}

func TestListArtifactsReturnsLatestAndApprovedVersions(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved", ""); err != nil {
		t.Fatal(err)
	}
	artifacts, err := db.ListArtifacts(ctx, "PROJECT-001", "prd")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d", len(artifacts))
	}
	if artifacts[0].LatestVersion != 1 || artifacts[0].ApprovedVersion != 1 || artifacts[0].Path != ".devagent/prd.md" {
		t.Fatalf("artifact = %#v", artifacts[0])
	}
	if artifacts[0].Content != "" {
		t.Fatalf("content should not be included by default: %#v", artifacts[0])
	}
}

func TestListArtifactsWithContentReturnsLatestSnapshot(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\nReview me before approval."),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := db.ListArtifactsWithContent(ctx, "PROJECT-001", "prd")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d", len(artifacts))
	}
	if artifacts[0].LatestVersionID != record.VersionID || artifacts[0].Content != "# PRD\n\nReview me before approval." || artifacts[0].ContentHash != record.Hash {
		t.Fatalf("artifact with content = %#v", artifacts[0])
	}
}

func TestSaveArtifactRevisionCreatesNewProposedVersionAndWritesProjectFile(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	root := t.TempDir()
	if _, err := db.SQL().ExecContext(ctx, "UPDATE projects SET root_path = ? WHERE id = ?", root, "PROJECT-001"); err != nil {
		t.Fatal(err)
	}
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
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", first.ArtifactID, first.Version, "rejected", "Needs metrics."); err != nil {
		t.Fatal(err)
	}

	revised, err := db.SaveArtifactRevision(ctx, "PROJECT-001", first.ArtifactID, []byte("# PRD\n\nSecond draft with metrics."))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Version != 2 || revised.Status != "proposed" {
		t.Fatalf("revised = %#v", revised)
	}
	content, err := os.ReadFile(filepath.Join(root, ".devagent", "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# PRD\n\nSecond draft with metrics." {
		t.Fatalf("project artifact content = %q", string(content))
	}
	var firstStatus string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM artifact_versions WHERE id = ?", first.VersionID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "superseded" {
		t.Fatalf("first status = %s", firstStatus)
	}
}

func TestApproveArtifactVersionSupersedesPreviousApprovedVersion(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture v1"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", first.ArtifactID, first.Version, "approved", ""); err != nil {
		t.Fatal(err)
	}
	second, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture v2"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", second.ArtifactID, second.Version, "approved", ""); err != nil {
		t.Fatal(err)
	}

	var firstStatus, approvedVersionID string
	if err := db.SQL().QueryRowContext(ctx, "SELECT status FROM artifact_versions WHERE id = ?", first.VersionID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT approved_version_id FROM artifacts WHERE id = ?", first.ArtifactID).Scan(&approvedVersionID); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "superseded" || approvedVersionID != second.VersionID {
		t.Fatalf("first status=%s approved=%s", firstStatus, approvedVersionID)
	}
	records, err := db.TrustedArtifactContext(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].VersionID != second.VersionID {
		t.Fatalf("trusted context = %#v", records)
	}
}

func TestApprovedWithNotesRequiresNotes(t *testing.T) {
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
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved_with_notes", ""); err == nil {
		t.Fatal("expected approved_with_notes without notes to fail")
	}
}

func TestTrustedArtifactContextUsesApprovedVersionsAndNotes(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	first, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\napproved"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	notes := "Keep local-first storage requirement."
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", first.ArtifactID, 1, "approved_with_notes", notes); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactPRD,
		Path:         ".devagent/prd.md",
		Content:      []byte("# PRD\n\nunapproved draft"),
		Status:       "proposed",
	}); err != nil {
		t.Fatal(err)
	}

	records, err := db.TrustedArtifactContext(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("trusted artifact count = %d", len(records))
	}
	record := records[0]
	if record.VersionID != first.VersionID || record.Version != 1 || record.Status != "approved_with_notes" {
		t.Fatalf("trusted artifact used wrong version: %#v", record)
	}
	if record.ApprovalNotes != notes {
		t.Fatalf("approval notes = %q", record.ApprovalNotes)
	}
	if record.ContentHash != first.Hash {
		t.Fatalf("content hash = %s, want %s", record.ContentHash, first.Hash)
	}
	if record.ReviewedAt == "" {
		t.Fatal("reviewed_at was not included")
	}
	bundle, err := db.TrustedArtifactContentBundle(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle) != 1 || bundle[0].Content != "# PRD\n\napproved" {
		t.Fatalf("trusted artifact content bundle = %#v", bundle)
	}
}

func TestTrustedArtifactContextExcludesUnapprovedArtifacts(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	if _, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactRoadmap,
		Path:         ".devagent/roadmap.yaml",
		Content:      []byte("roadmap: []"),
		Status:       "proposed",
	}); err != nil {
		t.Fatal(err)
	}

	records, err := db.TrustedArtifactContext(ctx, "PROJECT-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("trusted artifact count = %d, want 0", len(records))
	}
}

func TestTrustedArtifactContentBundleRejectsSnapshotHashMismatch(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	insertProject(t, db.SQL(), "PROJECT-001")
	record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
		ProjectID:    "PROJECT-001",
		ArtifactType: ArtifactArchitecture,
		Path:         ".devagent/architecture.md",
		Content:      []byte("# Architecture"),
		Status:       "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApproveArtifactVersion(ctx, "PROJECT-001", record.ArtifactID, 1, "approved", ""); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(db.dataRoot, artifactVersionSnapshotPath("PROJECT-001", record.ArtifactID, record.VersionID, record.Path))
	if err := os.WriteFile(snapshotPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := db.TrustedArtifactContentBundle(ctx, "PROJECT-001"); err == nil {
		t.Fatal("expected snapshot hash mismatch to fail")
	}
}
