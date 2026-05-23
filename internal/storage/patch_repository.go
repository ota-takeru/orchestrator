package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
	"github.com/ota-takeru/orchestrator/internal/runners"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type PatchApplicationRecord struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	PatchHash     string `json:"patch_hash"`
	AppliedCommit string `json:"applied_commit,omitempty"`
}

func (db *DB) ListPatchApplications(ctx context.Context, projectID string, taskID string) ([]PatchApplicationRecord, error) {
	query := `
SELECT id, task_id, status, patch_hash, applied_commit
FROM patch_applications
WHERE project_id = ?`
	args := []any{projectID}
	if strings.TrimSpace(taskID) != "" {
		query += " AND task_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patches []PatchApplicationRecord
	for rows.Next() {
		var patch PatchApplicationRecord
		var appliedCommit sql.NullString
		if err := rows.Scan(&patch.ID, &patch.TaskID, &patch.Status, &patch.PatchHash, &appliedCommit); err != nil {
			return nil, err
		}
		if appliedCommit.Valid {
			patch.AppliedCommit = appliedCommit.String
		}
		patches = append(patches, patch)
	}
	return patches, rows.Err()
}

func (db *DB) ExportPatch(ctx context.Context, projectID string, taskID string) (PatchApplicationRecord, error) {
	status, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	if status != "approved_for_merge" {
		return PatchApplicationRecord{}, fmt.Errorf("task %s is not approved for patch export: %s", taskID, status)
	}
	if err := statemachine.Task.ValidateTransition("approved_for_merge", "patch_exported"); err != nil {
		return PatchApplicationRecord{}, err
	}
	evidence, err := db.latestDiffEvidence(ctx, projectID, taskID)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	patchContent := []byte("diff --git a/fake.txt b/fake.txt\n")
	patchHash := sha256Hex(patchContent)
	patchID := "PATCH-" + stableShortHash(taskID+"|"+patchHash)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	artifact, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        evidence.RunID,
		ArtifactType: "diff",
		ArtifactKey:  "manual.patch",
		Content:      patchContent,
	}, now)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	payload, err := json.Marshal(map[string]any{"diff_hash": evidence.DiffHash, "head_commit": evidence.HeadCommit})
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO patch_applications(
  id, project_id, task_id, status, patch_artifact_id, patch_hash,
  evidence_json, created_at, updated_at
) VALUES (?, ?, ?, 'exported', ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = 'exported',
  patch_artifact_id = excluded.patch_artifact_id,
  patch_hash = excluded.patch_hash,
  evidence_json = excluded.evidence_json,
  updated_at = excluded.updated_at`,
		patchID, projectID, taskID, artifact.ID, patchHash, string(payload), now, now,
	); err != nil {
		return PatchApplicationRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'patch_exported', updated_at = ? WHERE project_id = ? AND id = ?", now, projectID, taskID); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "patch_exported", map[string]any{"task_id": taskID, "patch_application_id": patchID}, now); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return PatchApplicationRecord{}, err
	}
	committed = true
	return PatchApplicationRecord{ID: patchID, TaskID: taskID, Status: "exported", PatchHash: patchHash}, nil
}

func (db *DB) MarkPatchApplied(ctx context.Context, projectID string, taskID string, commit string) (PatchApplicationRecord, error) {
	if strings.TrimSpace(commit) == "" {
		return PatchApplicationRecord{}, fmt.Errorf("applied commit is required")
	}
	patch, err := db.openPatchApplication(ctx, projectID, taskID, "exported")
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := statemachine.Task.ValidateTransition("patch_exported", "manually_applied"); err != nil {
		return PatchApplicationRecord{}, err
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE patch_applications SET status = 'manually_applied', applied_commit = ?, updated_at = ? WHERE id = ?", commit, now, patch.ID); err != nil {
		return PatchApplicationRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'manually_applied', updated_at = ? WHERE project_id = ? AND id = ?", now, projectID, taskID); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "patch_manually_applied", map[string]any{"task_id": taskID, "patch_application_id": patch.ID, "commit": commit}, now); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return PatchApplicationRecord{}, err
	}
	committed = true
	patch.Status = "manually_applied"
	patch.AppliedCommit = commit
	return patch, nil
}

func (db *DB) VerifyAppliedPatchFake(ctx context.Context, projectID string, taskID string) (PatchApplicationRecord, error) {
	patch, err := db.openPatchApplication(ctx, projectID, taskID, "manually_applied")
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := statemachine.Task.ValidateTransition("manually_applied", "reverifying"); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := db.transitionTask(ctx, projectID, taskID, "manually_applied", "reverifying", "patch_verify_started", map[string]any{"patch_application_id": patch.ID}); err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := db.updatePatchStatus(ctx, projectID, patch.ID, "manually_applied", "verifying"); err != nil {
		return PatchApplicationRecord{}, err
	}
	env, err := db.primaryEnvironment(ctx, projectID)
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	runID := "RUN-" + stableShortHash(taskID+"|patch-reverify|"+time.Now().UTC().Format(time.RFC3339Nano))
	command := verifier.Command{ID: "fake-patch-reverify", EnvironmentID: env.ID, Runner: "fake", WorkingDir: env.ProjectRoot, Argv: []string{"reverify"}, NetworkPolicy: runners.NetworkOff, RequiredForMerge: true}
	report, err := verifier.Run(ctx, runID, verifier.StaticRunnerRegistry{env.ID: fakeRunnerForEnvironment(env)}, []verifier.Command{command})
	if err != nil {
		return PatchApplicationRecord{}, err
	}
	if err := db.SaveVerificationReport(ctx, SaveVerificationInput{
		ProjectID:           projectID,
		TaskID:              &taskID,
		RunID:               runID,
		RunType:             "reverify",
		AttemptNo:           1,
		BaseCommit:          "BASE",
		ReverifyContextType: "patch_application",
		ReverifyContextID:   patch.ID,
		Commands:            []verifier.Command{command},
		Report:              report,
	}); err != nil {
		return PatchApplicationRecord{}, err
	}
	gates := decisions.EvaluateVerification(report)
	if err := db.SaveGateResults(ctx, projectID, &taskID, runID, gates); err != nil {
		return PatchApplicationRecord{}, err
	}
	if taskStatusFromGateResults(gates) != "ready_for_human_review" {
		if err := db.updatePatchStatus(ctx, projectID, patch.ID, "verifying", "needs_decision"); err != nil {
			return PatchApplicationRecord{}, err
		}
		return PatchApplicationRecord{}, fmt.Errorf("applied patch verification did not pass")
	}
	if err := db.markPatchVerified(ctx, projectID, taskID, patch.ID); err != nil {
		return PatchApplicationRecord{}, err
	}
	patch.Status = "verified"
	return patch, nil
}

func (db *DB) latestDiffEvidence(ctx context.Context, projectID string, taskID string) (approvalEvidence, error) {
	var evidence approvalEvidence
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, base_commit, head_commit, diff_hash
FROM runs
WHERE project_id = ? AND task_id = ? AND status = 'succeeded'
  AND head_commit IS NOT NULL AND diff_hash IS NOT NULL
ORDER BY created_at DESC
LIMIT 1`, projectID, taskID).Scan(&evidence.RunID, &evidence.BaseCommit, &evidence.HeadCommit, &evidence.DiffHash); err != nil {
		if err == sql.ErrNoRows {
			return approvalEvidence{}, fmt.Errorf("diff evidence not found for task %s", taskID)
		}
		return approvalEvidence{}, err
	}
	return evidence, nil
}

func (db *DB) openPatchApplication(ctx context.Context, projectID string, taskID string, status string) (PatchApplicationRecord, error) {
	var patch PatchApplicationRecord
	var appliedCommit sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
SELECT id, task_id, status, patch_hash, applied_commit
FROM patch_applications
WHERE project_id = ? AND task_id = ? AND status = ?
ORDER BY created_at DESC
LIMIT 1`, projectID, taskID, status).Scan(&patch.ID, &patch.TaskID, &patch.Status, &patch.PatchHash, &appliedCommit); err != nil {
		if err == sql.ErrNoRows {
			return PatchApplicationRecord{}, fmt.Errorf("patch application not found for task %s in status %s", taskID, status)
		}
		return PatchApplicationRecord{}, err
	}
	if appliedCommit.Valid {
		patch.AppliedCommit = appliedCommit.String
	}
	return patch, nil
}

func (db *DB) updatePatchStatus(ctx context.Context, projectID string, patchID string, from string, to string) error {
	if err := statemachine.PatchApplication.ValidateTransition(from, to); err != nil {
		return err
	}
	_, err := db.sql.ExecContext(ctx, "UPDATE patch_applications SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status = ?", to, time.Now().UTC().Format(time.RFC3339Nano), projectID, patchID, from)
	return err
}

func (db *DB) markPatchVerified(ctx context.Context, projectID string, taskID string, patchID string) error {
	if err := statemachine.PatchApplication.ValidateTransition("verifying", "verified"); err != nil {
		return err
	}
	if err := statemachine.Task.ValidateTransition("reverifying", "applied"); err != nil {
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
	if _, err := tx.ExecContext(ctx, "UPDATE patch_applications SET status = 'verified', updated_at = ?, completed_at = ? WHERE project_id = ? AND id = ? AND status = 'verifying'", now, now, projectID, patchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'applied', updated_at = ? WHERE project_id = ? AND id = ? AND status = 'reverifying'", now, projectID, taskID); err != nil {
		return err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "patch_verified_applied", map[string]any{"task_id": taskID, "patch_application_id": patchID}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
