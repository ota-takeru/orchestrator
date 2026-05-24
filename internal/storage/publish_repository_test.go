package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishDryRunCapturesRemoteReadiness(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "--bare", remote)
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "origin", "main")

	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)

	result, err := db.PublishDryRun(ctx, projectID, PublishDryRunInput{Remote: "origin", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Relation != "up_to_date" || result.RemoteOID == "" || result.LocalOID == "" {
		t.Fatalf("publish dry-run = %#v", result)
	}
	var artifactKey string
	if err := db.SQL().QueryRowContext(ctx, "SELECT artifact_key FROM run_artifacts WHERE run_id = ? AND artifact_type = 'summary'", result.RunID).Scan(&artifactKey); err != nil {
		t.Fatal(err)
	}
	if artifactKey != "publish-dry-run-summary.json" {
		t.Fatalf("artifact key = %s", artifactKey)
	}
}

func TestPublishDryRunBlocksDivergedRemote(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "--bare", remote)
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "origin", "main")
	clone := filepath.Join(t.TempDir(), "clone")
	gitRun(t, "", "clone", remote, clone)
	gitRun(t, clone, "config", "user.email", "test@example.com")
	gitRun(t, clone, "config", "user.name", "Test User")
	writeAndCommit(t, clone, "remote.txt", "remote\n")
	gitRun(t, clone, "push", "origin", "main")
	writeAndCommit(t, repo, "local.txt", "local\n")

	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)

	result, err := db.PublishDryRun(ctx, projectID, PublishDryRunInput{Remote: "origin", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Relation != "diverged" || len(result.Blockers) == 0 {
		t.Fatalf("publish dry-run = %#v", result)
	}
}

func TestPublishExecutePushesLocalAheadBranch(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	repo := initStorageGitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "--bare", remote)
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "origin", "main")
	writeAndCommit(t, repo, "local.txt", "local\n")
	localOID := gitOutput(t, repo, "rev-parse", "refs/heads/main")

	projectID := "PROJECT-001"
	insertProjectWithRoot(t, db, projectID, repo)
	insertEnvironmentWithRoot(t, db, "linux-main", projectID, "primary", repo)

	result, err := db.PublishExecute(ctx, projectID, PublishExecuteInput{Remote: "origin", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.RelationBefore != "local_ahead" || result.RemoteOIDAfter != localOID {
		t.Fatalf("publish execute = %#v", result)
	}
	remoteOID := gitOutput(t, repo, "ls-remote", "--heads", "origin", "main")
	if !strings.HasPrefix(remoteOID, localOID) {
		t.Fatalf("remote oid output = %s, want prefix %s", remoteOID, localOID)
	}
}

func writeAndCommit(t *testing.T, repo string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", name)
	gitRun(t, repo, "commit", "-m", "update "+name)
}
