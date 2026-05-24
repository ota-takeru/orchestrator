package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/verifier"
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
	ReverifyRunID     string   `json:"reverify_run_id,omitempty"`
	Target            string   `json:"target"`
	PreMainOID        string   `json:"pre_main_oid"`
	CandidateOID      string   `json:"candidate_oid"`
	Blockers          []string `json:"blockers,omitempty"`
	FailureClass      string   `json:"failure_class,omitempty"`
	RollbackStatus    string   `json:"rollback_status,omitempty"`
	RollbackError     string   `json:"rollback_error,omitempty"`
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
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, Blockers: blockers, FailureClass: "merge_blocker"}, nil
	}
	status := strings.TrimSpace(gitOutputOrEmpty(ctx, env.ProjectRoot, "status", "--porcelain=v1"))
	if status != "" {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, Blockers: []string{"main worktree is not clean"}, FailureClass: "worktree_dirty"}, nil
	}
	if ok, err := db.hasSavedDiffArtifact(ctx, projectID, entry.TaskID); err != nil {
		return RealGitMergeResult{}, err
	} else if !ok {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, Blockers: []string{"diff artifact is not saved"}, FailureClass: "missing_diff_artifact"}, nil
	}
	preMain := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "refs/heads/"+input.Target)
	candidate := gitOutputOrUnknown(ctx, env.ProjectRoot, "rev-parse", "--verify", entry.HeadCommit+"^{commit}")
	if preMain == "UNKNOWN" || candidate == "UNKNOWN" {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: []string{"target or candidate commit is missing"}, FailureClass: "missing_commit"}, nil
	}
	if err := runGit(ctx, env.ProjectRoot, "merge-base", "--is-ancestor", preMain, candidate); err != nil {
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "blocked", Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: []string{"candidate is not a fast-forward descendant of target"}, FailureClass: "non_fast_forward"}, nil
	}

	reverifyRunID, reverifyBlockers, err := db.reverifyRealMergeCandidate(ctx, projectID, entry, env, preMain, candidate)
	if err != nil {
		return RealGitMergeResult{}, err
	}
	if len(reverifyBlockers) > 0 {
		return RealGitMergeResult{
			MergeQueueEntryID: entry.ID,
			TaskID:            entry.TaskID,
			Status:            "blocked",
			ReverifyRunID:     reverifyRunID,
			Target:            input.Target,
			PreMainOID:        preMain,
			CandidateOID:      candidate,
			Blockers:          reverifyBlockers,
			FailureClass:      "reverification_failed",
		}, nil
	}

	runID := "RUN-" + stableShortHash(entry.TaskID+"|real-git-merge|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, entry.TaskID, "merge")
	if err != nil {
		return RealGitMergeResult{}, err
	}
	if err := runGit(ctx, env.ProjectRoot, "update-ref", "refs/heads/"+input.Target, candidate, preMain); err != nil {
		blockers := []string{"target ref changed before update"}
		_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", blockers, "ref_update_failed", "", "")
		return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "failed", RunID: runID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: blockers, FailureClass: "ref_update_failed"}, nil
	}
	currentBranch := strings.TrimSpace(gitOutputOrEmpty(ctx, env.ProjectRoot, "symbolic-ref", "--quiet", "--short", "HEAD"))
	if currentBranch == input.Target {
		if err := runGit(ctx, env.ProjectRoot, "reset", "--hard", candidate); err != nil {
			rollbackStatus, rollbackError := rollbackLocalRefStatus(ctx, env.ProjectRoot, input.Target, preMain, candidate, true)
			blockers := []string{"worktree reset after fast-forward failed"}
			if rollbackStatus == "failed" {
				blockers = append(blockers, "rollback to pre-merge ref failed")
			}
			_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", blockers, "worktree_reset_failed", rollbackStatus, rollbackError)
			return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "failed", RunID: runID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate, Blockers: blockers, FailureClass: "worktree_reset_failed", RollbackStatus: rollbackStatus, RollbackError: rollbackError}, nil
		}
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "queued", "rebasing"); err != nil {
		rollbackStatus, rollbackError := rollbackLocalRefStatus(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", realGitDBSyncBlockers(rollbackStatus), "db_state_sync_failed", rollbackStatus, rollbackError)
		return RealGitMergeResult{}, err
	}
	if err := db.syncMergeQueueState(ctx, projectID, entry.ID, entry.TaskID, "rebasing", "reverifying"); err != nil {
		rollbackStatus, rollbackError := rollbackLocalRefStatus(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", realGitDBSyncBlockers(rollbackStatus), "db_state_sync_failed", rollbackStatus, rollbackError)
		return RealGitMergeResult{}, err
	}
	if err := db.markMergeQueueMerged(ctx, projectID, entry.ID, entry.TaskID); err != nil {
		rollbackStatus, rollbackError := rollbackLocalRefStatus(ctx, env.ProjectRoot, input.Target, preMain, candidate, currentBranch == input.Target)
		_ = db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "failed", realGitDBSyncBlockers(rollbackStatus), "db_state_sync_failed", rollbackStatus, rollbackError)
		return RealGitMergeResult{}, err
	}
	if err := db.saveRealGitMergeEvidence(ctx, projectID, entry, runID, attemptNo, input.Target, preMain, candidate, "succeeded", nil, "", "", ""); err != nil {
		return RealGitMergeResult{}, err
	}
	return RealGitMergeResult{MergeQueueEntryID: entry.ID, TaskID: entry.TaskID, Status: "succeeded", RunID: runID, ReverifyRunID: reverifyRunID, Target: input.Target, PreMainOID: preMain, CandidateOID: candidate}, nil
}

func (db *DB) reverifyRealMergeCandidate(ctx context.Context, projectID string, entry MergeQueueEntry, env platform.ExecutionEnvironment, preMain string, candidate string) (string, []string, error) {
	tempRoot, err := os.MkdirTemp("", "devos-real-merge-reverify-*")
	if err != nil {
		return "", nil, err
	}
	worktreeAdded := false
	defer func() {
		if worktreeAdded {
			if err := runGit(context.Background(), env.ProjectRoot, "worktree", "remove", "--force", tempRoot); err != nil {
				_ = os.RemoveAll(tempRoot)
				_ = runGit(context.Background(), env.ProjectRoot, "worktree", "prune")
			}
			return
		}
		_ = os.RemoveAll(tempRoot)
	}()
	if err := runGit(ctx, env.ProjectRoot, "worktree", "add", "--detach", tempRoot, candidate); err != nil {
		return "", []string{"integration worktree creation failed"}, nil
	}
	worktreeAdded = true

	verifyEnv := env
	verifyEnv.ProjectRoot = tempRoot
	runID := "RUN-" + stableShortHash(entry.TaskID+"|real-merge-reverify|"+time.Now().UTC().Format(time.RFC3339Nano))
	attemptNo, err := db.nextRunAttempt(ctx, projectID, entry.TaskID, "reverify")
	if err != nil {
		return "", nil, err
	}
	commands := defaultLocalVerificationCommands(ctx, verifyEnv)
	report, err := verifier.Run(ctx, runID, verifier.StaticRunnerRegistry{verifyEnv.ID: runners.NewLocalRunner(verifyEnv)}, commands)
	if err != nil {
		return "", nil, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:           projectID,
		TaskID:              &entry.TaskID,
		RunID:               runID,
		RunType:             "reverify",
		AttemptNo:           attemptNo,
		BaseCommit:          preMain,
		ReverifyContextType: "merge_queue_entry",
		ReverifyContextID:   entry.ID,
		Commands:            commands,
		Report:              report,
	}); err != nil {
		return "", nil, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &entry.TaskID, runID, gates); err != nil {
		return "", nil, err
	}
	if taskStatusFromGateResults(gates) != "ready_for_human_review" {
		return runID, []string{"merge reverification did not pass"}, nil
	}
	return runID, nil, nil
}

func (db *DB) unresolvedMergeBlockers(ctx context.Context, projectID string) ([]string, error) {
	if err := db.RevokeExpiredToolchainWaivers(ctx, projectID, time.Now().UTC()); err != nil {
		return nil, err
	}
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
	if err := db.sql.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM toolchain_requirements
WHERE project_id = ?
  AND required_for_merge = 1
  AND status IN ('missing', 'invalid', 'setup_required', 'unsupported', 'revoked')`, projectID).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return []string{"required toolchain is not ready for merge"}, nil
	}
	return nil, nil
}

func (db *DB) mergeBlockingInboxItems(ctx context.Context, projectID string) ([]InboxItem, error) {
	items, err := db.ListInboxItems(ctx, projectID, "open")
	if err != nil {
		return nil, err
	}
	filtered := make([]InboxItem, 0, len(items))
	for _, item := range items {
		switch item.SourceType {
		case "decision", "human_approval", "merge_conflict":
			filtered = append(filtered, item)
		case "toolchain_requirement":
			required, err := db.toolchainRequiredForMerge(ctx, projectID, item.SourceID)
			if err != nil {
				return nil, err
			}
			if required {
				filtered = append(filtered, item)
			}
		}
	}
	return filtered, nil
}

func (db *DB) toolchainRequiredForMerge(ctx context.Context, projectID string, requirementID string) (bool, error) {
	var required int
	err := db.sql.QueryRowContext(ctx, "SELECT required_for_merge FROM toolchain_requirements WHERE project_id = ? AND id = ?", projectID, requirementID).Scan(&required)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return required == 1, nil
}

func (db *DB) saveRealGitMergeEvidence(ctx context.Context, projectID string, entry MergeQueueEntry, runID string, attemptNo int, target string, preMain string, candidate string, status string, blockers []string, failureClass string, rollbackStatus string, rollbackError string) error {
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
	summaryMap := map[string]any{
		"merge_queue_entry_id": entry.ID,
		"task_id":              entry.TaskID,
		"target":               target,
		"pre_main_oid":         preMain,
		"candidate_oid":        candidate,
		"status":               status,
		"ff_only":              true,
		"no_push":              true,
		"blockers":             blockers,
	}
	if failureClass != "" {
		summaryMap["failure_class"] = failureClass
	}
	if rollbackStatus != "" {
		summaryMap["rollback_status"] = rollbackStatus
	}
	if rollbackError != "" {
		summaryMap["rollback_error"] = rollbackError
	}
	summary, err := json.MarshalIndent(summaryMap, "", "  ")
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
	eventEvidence := map[string]any{
		"task_id":              entry.TaskID,
		"merge_queue_entry_id": entry.ID,
		"run_id":               runID,
		"target":               target,
		"pre_main_oid":         preMain,
		"candidate_oid":        candidate,
		"blockers":             blockers,
	}
	if failureClass != "" {
		eventEvidence["failure_class"] = failureClass
	}
	if rollbackStatus != "" {
		eventEvidence["rollback_status"] = rollbackStatus
	}
	if rollbackError != "" {
		eventEvidence["rollback_error"] = rollbackError
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "real_git_merge_"+status, eventEvidence, now); err != nil {
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

func rollbackLocalRefStatus(ctx context.Context, repo string, target string, preMain string, candidate string, resetWorktree bool) (string, string) {
	if err := rollbackLocalRef(ctx, repo, target, preMain, candidate, resetWorktree); err != nil {
		return "failed", err.Error()
	}
	return "succeeded", ""
}

func realGitDBSyncBlockers(rollbackStatus string) []string {
	blockers := []string{"database merge state update failed after target ref update"}
	if rollbackStatus == "failed" {
		blockers = append(blockers, "rollback to pre-merge ref failed")
	}
	return blockers
}

func runGit(ctx context.Context, repo string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
