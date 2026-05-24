package storage

import (
	"context"
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
