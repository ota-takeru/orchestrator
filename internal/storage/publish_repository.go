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

type PublishExecuteInput struct {
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

type PublishExecuteResult struct {
	RunID           string   `json:"run_id"`
	Status          string   `json:"status"`
	Remote          string   `json:"remote"`
	Branch          string   `json:"branch"`
	RemoteURL       string   `json:"remote_url"`
	LocalOID        string   `json:"local_oid"`
	RemoteOIDBefore string   `json:"remote_oid_before"`
	RemoteOIDAfter  string   `json:"remote_oid_after"`
	RelationBefore  string   `json:"relation_before"`
	Blockers        []string `json:"blockers,omitempty"`
}

func (db *DB) PublishDryRun(ctx context.Context, projectID string, input PublishDryRunInput) (PublishDryRunResult, error) {
	result, err := db.collectPublishReadiness(ctx, projectID, input.Remote, input.Branch)
	if err != nil {
		return PublishDryRunResult{}, err
	}
	result.RunID = "RUN-" + stableShortHash(projectID+"|publish-dry-run|"+result.Remote+"|"+result.Branch+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	if err := db.savePublishDryRunEvidence(ctx, projectID, result); err != nil {
		return PublishDryRunResult{}, err
	}
	return result, nil
}

func (db *DB) PublishExecute(ctx context.Context, projectID string, input PublishExecuteInput) (PublishExecuteResult, error) {
	readiness, err := db.collectPublishReadiness(ctx, projectID, input.Remote, input.Branch)
	if err != nil {
		return PublishExecuteResult{}, err
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
	if err != nil {
		return PublishExecuteResult{}, err
	}
	runID := "RUN-" + stableShortHash(projectID+"|publish-execute|"+readiness.Remote+"|"+readiness.Branch+"|"+time.Now().UTC().Format(time.RFC3339Nano))
	result := PublishExecuteResult{
		RunID:           runID,
		Status:          "succeeded",
		Remote:          readiness.Remote,
		Branch:          readiness.Branch,
		RemoteURL:       readiness.RemoteURL,
		LocalOID:        readiness.LocalOID,
		RemoteOIDBefore: readiness.RemoteOID,
		RemoteOIDAfter:  readiness.RemoteOID,
		RelationBefore:  readiness.Relation,
		Blockers:        append([]string{}, readiness.Blockers...),
	}
	if len(result.Blockers) == 0 && readiness.Relation != "local_ahead" && readiness.Relation != "up_to_date" {
		result.Blockers = append(result.Blockers, "local branch is not publishable")
	}
	if len(result.Blockers) == 0 && readiness.Relation == "local_ahead" {
		if err := runGit(ctx, env.ProjectRoot, "push", readiness.Remote, "refs/heads/"+readiness.Branch+":refs/heads/"+readiness.Branch); err != nil {
			result.Blockers = append(result.Blockers, "git push failed: "+err.Error())
		}
	}
	if len(result.Blockers) == 0 {
		remoteOIDAfter, remoteBlocker := gitRemoteHeadOID(ctx, env.ProjectRoot, readiness.Remote, readiness.Branch)
		result.RemoteOIDAfter = remoteOIDAfter
		if remoteBlocker != "" {
			result.Blockers = append(result.Blockers, remoteBlocker)
		} else if remoteOIDAfter != readiness.LocalOID {
			result.Blockers = append(result.Blockers, "remote oid after push does not match local oid")
		}
	}
	if len(result.Blockers) > 0 {
		result.Status = "blocked"
	}
	if err := db.savePublishExecuteEvidence(ctx, projectID, result); err != nil {
		return PublishExecuteResult{}, err
	}
	return result, nil
}

func (db *DB) collectPublishReadiness(ctx context.Context, projectID string, remoteInput string, branchInput string) (PublishDryRunResult, error) {
	remote := strings.TrimSpace(remoteInput)
	if remote == "" {
		remote = "origin"
	}
	branch := strings.TrimSpace(branchInput)
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
	return PublishDryRunResult{
		Status:    status,
		Remote:    remote,
		Branch:    branch,
		RemoteURL: remoteURL,
		LocalOID:  localOID,
		RemoteOID: remoteOID,
		Relation:  relation,
		Blockers:  blockers,
	}, nil
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

func (db *DB) savePublishExecuteEvidence(ctx context.Context, projectID string, result PublishExecuteResult) error {
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
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET head_commit = ? WHERE project_id = ? AND id = ?", result.RemoteOIDAfter, projectID, result.RunID); err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        result.RunID,
		ArtifactType: "summary",
		ArtifactKey:  "publish-execute-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "publish_execute", map[string]any{
		"run_id":            result.RunID,
		"status":            result.Status,
		"remote":            result.Remote,
		"branch":            result.Branch,
		"relation_before":   result.RelationBefore,
		"remote_oid_before": result.RemoteOIDBefore,
		"remote_oid_after":  result.RemoteOIDAfter,
		"blockers":          result.Blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
