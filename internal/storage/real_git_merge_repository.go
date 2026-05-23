package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type RealGitMergeInput struct {
	EntryID string
	Target  string
	Execute bool
	FFOnly  bool
	NoPush  bool
}

type RealGitMergeResult struct {
	MergeQueueEntryID string   `json:"merge_queue_entry_id"`
	TaskID            string   `json:"task_id"`
	Status            string   `json:"status"`
	RunID             string   `json:"run_id"`
	Target            string   `json:"target"`
	PreMainOID        string   `json:"pre_main_oid"`
	CandidateOID      string   `json:"candidate_oid"`
	Blockers          []string `json:"blockers,omitempty"`
}

func (db *DB) ProcessRealGitMerge(ctx context.Context, projectID string, input RealGitMergeInput) (RealGitMergeResult, error) {
	if !input.Execute {
		return RealGitMergeResult{}, fmt.Errorf("real git merge requires --execute")
	}
	if !input.FFOnly {
		return RealGitMergeResult{}, fmt.Errorf("real git merge v1 requires --ff-only")
	}
	if !input.NoPush {
		return RealGitMergeResult{}, fmt.Errorf("real git merge v1 requires --no-push")
	}
	if strings.TrimSpace(input.Target) == "" {
		input.Target = "main"
	}
	entry, err := db.openMergeEntryForGitDryRun(ctx, projectID, input.EntryID)
	if err != nil {
		return RealGitMergeResult{}, err
	}
	env, err := db.ResolveCanonicalGitEnvironment(ctx, projectID)
	if err != nil {
		return RealGitMergeResult{}, err
	}
	if blockers, err := db.unresolvedMergeBlockers(ctx, projectID); err != nil {
		return RealGitMergeResult{}, err
	} else if len(blockers) > 0 {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, Blockers: blockers}, nil
	}
	status := strings.TrimSpace(gitOutputOrEmpty(ctx, env.ProjectRoot, "status", "--porcelain=v1"))
	if status != "" {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, Blockers: []string{"main worktree is not clean"}}, nil
	}
	preMain := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "refs/heads/"+input.Target)
	candidate := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "--verify", entry.HeadCommit+"^{commit}")
	if preMain == "UNKNOWN" || candidate == "UNKNOWN" {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: []string{"target or candidate commit is missing"}}, nil
	}
	if err := runGit(ctx, env.ProjectRoot, "merge-base", "--is-ancestor", preMain, candidate); err != nil {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: []string{"candidate is not a fast-forward descendant of target"}}, nil
	}

	runID := "RUN-" + stableShortHash(entry.TaskID+"|real-git-merge|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, entry.TaskID, "merge")
	if err != nil {
		return RealGitMergeResult{}, err
	}
	if err := runGit(ctx, env.ProjectRoot, "update-ref", "refs/heads/"+input.Target, candidate, preMain); err != nil {
		blockers := []string{"target ref changed before update"}
		_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", blockers)
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "failed", RunID: runID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: blockers}, nil
	}
	currentBranch := strings.TrimSpace(gitOutputOrEmpty(ctx, env.ProjectRoot, "symbolic-ref", "--quiet", "--short", "HEAD"))
	if currentBranch == input.Target {
		if err := runGit(ctx, env.ProjectRoot, "reset", "--hard", candidate); err != nil {
			_ = runGit(ctx, env.ProjectRoot, "update-ref", "refs/heads/"+input.Target, preMain, candidate)
			blockers := []string{"worktree reset after fast-forward failed"}
			_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", blockers)
			return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "failed", RunID: runID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: blockers}, nil
		}
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "queued", "rebasing"); err != nil {
		_ = rollbackLocalRef(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		return RealGitMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "rebasing", "reverifying"); err != nil {
		_ = rollbackLocalRef(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		return RealGitMergeResult{}, err
	}
	if err := db.markMergeQueueMerged(ctx, projectID, entry.ID, entry.TaskID); err != nil {
		_ = rollbackLocalRef(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		return RealGitMergeResult{}, err
	}
	if err := db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "succeeded", nil); err != nil {
		return RealGitMergeResult{}, err
	}
	return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "succeeded", RunID: runID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate}, nil
}

func (db *DB) unresolvedMergeBlockers(ctx context.Context, projectID string) ([]string, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM inbox_items ii
LEFT JOIN toolchain_requirements tr ON tr.project_id = ii.project_id AND tr.id = ii.source_id
WHERE ii.project_id = ? AND ii.status = 'open' AND (
  ii.source_type IN ('decision', 'human_approval', 'merge_conflict')
  OR (ii.source_type = 'toolchain_requirement' AND tr.required_for_merge = 1)
)`, projectID).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return []string{"unresolved blocking inbox items exist"}, nil
	}
	return nil, nil
}

func (db *DB) saveRealGitMergeEvidence(ctx context.Context, projectID string, entry MergeQueueEntry, runID string, attemptNo int, target string, preMain string, candidate string, status string, blockers []string) error {
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
	if err := insertRun(ctx, tx, SaveVerificationInput{
		ProjectID:  projectID,
		TaskID:     &entry.TaskID,
		RunID:      runID,
		RunType:    "merge",
		AttemptNo:  attemptNo,
		BaseCommit: preMain,
	}, status, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET head_commit = ? WHERE project_id = ? AND id = ?", candidate, projectID, runID); err != nil {
		return err
	}
	summary, err := json.MarshalIndent(map[string]any{
		"merge_queue_entry_id": entry.ID,
		"task_id":              entry.TaskID,
		"target":               target,
		"pre_main_oid":         preMain,
		"candidate_oid":        candidate,
		"status":               status,
		"ff_only":              true,
		"no_push":              true,
		"blockers":             blockers,
	}, "", "  ")
	if err != nil {
		return err
	}
	if _, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        runID,
		ArtifactType: "summary",
		ArtifactKey:  "real-git-merge-summary.json",
		Content:      summary,
	}, now); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "real_git_merge_"+status, map[string]any{
		"task_id":              entry.TaskID,
		"merge_queue_entry_id": entry.ID,
		"run_id":               runID,
		"target":               target,
		"pre_main_oid":         preMain,
		"candidate_oid":        candidate,
		"blockers":             blockers,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func rollbackLocalRef(ctx context.Context, repo string, target string, preMain string, candidate string, resetWorktree bool) error {
	if err := runGit(ctx, repo, "update-ref", "refs/heads/"+target, preMain, candidate); err != nil {
		return err
	}
	if resetWorktree {
		return runGit(ctx, repo, "reset", "--hard", preMain)
	}
	return nil
}

func runGit(ctx context.Context, repo string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
