package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type PublishDryRunInput struct {
	Remote string
	Branch string
}

type PublishDryRunResult struct {
	RunID     string   `json:"run_id"`
	Status    string   `json:"status"`
	Remote    string   `json:"remote"`
	Branch    string   `json:"branch"`
	RemoteURL string   `json:"remote_url"`
	LocalOID  string   `json:"local_oid"`
	RemoteOID string   `json:"remote_oid"`
	Relation  string   `json:"relation"`
	Blockers  []string `json:"blockers,omitempty"`
}

func (db *DB) PublishDryRun(ctx context.Context, projectID string, input PublishDryRunInput) (PublishDryRunResult, error) {
	remote := strings.TrimSpace(input.Remote)
	if remote == "" {
		remote = "origin"
	}
	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		branch = "main"
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
	if err != nil {
		return PublishDryRunResult{}, err
	}
	remoteURL, remoteURLBlocker := gitOutputOrBlocker(ctx, env.ProjectRoot, "remote", "get-url", remote)
	localOID, localBlocker := gitOutputOrBlocker(ctx, env.ProjectRoot, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	remoteOID, remoteBlocker := gitRemoteHeadOID(ctx, env.ProjectRoot, remote, branch)
	blockers := []string{}
	if remoteURLBlocker != "" {
		blockers = append(blockers, remoteURLBlocker)
	}
	if localBlocker != "" {
		blockers = append(blockers, localBlocker)
	}
	if remoteBlocker != "" {
		blockers = append(blockers, remoteBlocker)
	}
	relation := "unknown"
	if len(blockers) == 0 {
		relation = publishRelation(ctx, env.ProjectRoot, localOID, remoteOID)
		if relation == "remote_ahead" || relation == "diverged" {
			blockers = append(blockers, "remote is not fast-forwardable from local branch")
		}
	}
	status := "succeeded"
	if len(blockers) > 0 {
		status = "blocked"
	}
	runID := "RUN-" + stableShortHash(projectID+"|publish-dry-run|"+remote+"|"+branch+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	result := PublishDryRunResult{
		RunID:     runID,
		Status:    status,
		Remote:    remote,
		Branch:    branch,
		RemoteURL: remoteURL,
		LocalOID:  localOID,
		RemoteOID: remoteOID,
		Relation:  relation,
		Blockers:  blockers,
	}
	if err := db.savePublishDryRunEvidence(ctx, projectID, result); err != nil {
		return PublishDryRunResult{}, err
	}
	return result, nil
}

func gitOutputOrBlocker(ctx context.Context, repo string, args ...string) (string, string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Sprintf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), ""
}

func gitRemoteHeadOID(ctx context.Context, repo string, remote string, branch string) (string, string) {
	out, blocker := gitOutputOrBlocker(ctx, repo, "ls-remote", "--heads", remote, branch)
	if blocker != "" {
		return "", blocker
	}
	if strings.TrimSpace(out) == "" {
		return "", "remote branch is missing"
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", "remote branch output was empty"
	}
	return fields[0], ""
}

func publishRelation(ctx context.Context, repo string, localOID string, remoteOID string) string {
	switch {
	case localOID == remoteOID:
		return "up_to_date"
	case runGit(ctx, repo, "merge-base", "--is-ancestor", remoteOID, localOID) == nil:
		return "local_ahead"
	case runGit(ctx, repo, "merge-base", "--is-ancestor", localOID, remoteOID) == nil:
		return "remote_ahead"
	default:
		return "diverged"
	}
}

func (db *DB) savePublishDryRunEvidence(ctx context.Context, projectID string, result PublishDryRunResult) error {
	attemptNo, err := db.nextProjectRunAttempt(ctx, projectID, "publish")
	if err != nil {
		return err
	}
	summary, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	runStatus := "succeeded"
	if result.Status == "blocked" {
		runStatus = "failed"
	}
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		RunID:      result.RunID,
		RunType:    "publish",
		AttemptNo:  attemptNo,
		BaseCommit: result.LocalOID,
	}, runStatus, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET head_commit = ? WHERE project_id = ? AND id = ?", result.RemoteOID, projectID, result.RunID); err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        result.RunID,
		ArtifactType: "summary",
		ArtifactKey:  "publish-dry-run-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "publish_dry_run", map[string]any{
		"run_id":   result.RunID,
		"status":   result.Status,
		"remote":   result.Remote,
		"branch":   result.Branch,
		"relation": result.Relation,
		"blockers": result.Blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
