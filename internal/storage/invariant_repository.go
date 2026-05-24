package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type InvariantViolation struct {
	Scope   string `json:"scope"`
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (db *DB) CheckArtifactInvariants(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	var violations []InvariantViolation
	checks := []func(context.Context, string) ([]InvariantViolation, error){
		db.checkApprovedVersionReferences,
		db.checkLatestVersionReferences,
		db.checkApprovedVersionStatuses,
		db.checkApprovedWithNotes,
		db.checkReadyTaskArtifactContext,
	}
	for _, check := range checks {
		found, err := check(ctx, projectID)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	return violations, nil
}

func (db *DB) CheckProjectInvariants(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	violations, err := db.CheckArtifactInvariants(ctx, projectID)
	if err != nil {
		return nil, err
	}
	checks := []func(context.Context, string) ([]InvariantViolation, error){
		db.checkPrimaryEnvironmentCount,
		db.checkRunProfileEnvironmentReferences,
		db.checkTaskVerificationCommands,
		db.checkTaskCurrentRunReferences,
		db.checkConcurrentRunningRuns,
		db.checkVerificationRunTypes,
		db.checkRequiredVerificationFailuresClassified,
		db.checkRunArtifactFiles,
		db.checkPathMappingServiceBuilds,
	}
	for _, check := range checks {
		found, err := check(ctx, projectID)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	return violations, nil
}

func (db *DB) checkPrimaryEnvironmentCount(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM execution_environments
WHERE project_id = ? AND role = 'primary'`, projectID).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return []InvariantViolation{{
			Scope:   "project",
			ID:      projectID,
			Code:    "primary_environment_count_invalid",
			Message: fmt.Sprintf("project requires exactly one primary environment, found %d", count),
		}}, nil
	}
	return nil, nil
}

func (db *DB) checkRunProfileEnvironmentReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	type profileReference struct {
		id             string
		primary        sql.NullString
		implementation sql.NullString
		git            sql.NullString
		merge          sql.NullString
		requiredJSON   string
		optionalJSON   string
	}
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, primary_environment_id, implementation_environment_id, git_environment_id,
       merge_environment_id, required_verification_environment_ids_json,
       optional_verification_environment_ids_json
FROM project_run_profiles
WHERE project_id = ? AND status = 'active'`, projectID)
	if err != nil {
		return nil, err
	}
	var profiles []profileReference
	for rows.Next() {
		var profile profileReference
		if err := rows.Scan(&profile.id, &profile.primary, &profile.implementation, &profile.git, &profile.merge, &profile.requiredJSON, &profile.optionalJSON); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var violations []InvariantViolation
	for _, profile := range profiles {
		for label, envID := range map[string]sql.NullString{
			"primary":        profile.primary,
			"implementation": profile.implementation,
			"git":            profile.git,
			"merge":          profile.merge,
		} {
			if !envID.Valid || strings.TrimSpace(envID.String) == "" {
				violations = append(violations, InvariantViolation{
					Scope:   "run_profile",
					ID:      profile.id,
					Code:    "run_profile_environment_reference_missing",
					Message: label + " environment id is required",
				})
				continue
			}
			if ok, err := db.environmentExists(ctx, projectID, envID.String); err != nil {
				return nil, err
			} else if !ok {
				violations = append(violations, InvariantViolation{
					Scope:   "run_profile",
					ID:      profile.id,
					Code:    "run_profile_environment_reference_invalid",
					Message: fmt.Sprintf("%s environment %s does not exist", label, envID.String),
				})
			}
		}
		for label, raw := range map[string]string{
			"required": profile.requiredJSON,
			"optional": profile.optionalJSON,
		} {
			envIDs, err := decodeJSONStringArray(raw)
			if err != nil {
				violations = append(violations, InvariantViolation{
					Scope:   "run_profile",
					ID:      profile.id,
					Code:    "run_profile_environment_ids_json_invalid",
					Message: fmt.Sprintf("%s verification environment ids are invalid: %v", label, err),
				})
				continue
			}
			for _, envID := range envIDs {
				if ok, err := db.environmentExists(ctx, projectID, envID); err != nil {
					return nil, err
				} else if !ok {
					violations = append(violations, InvariantViolation{
						Scope:   "run_profile",
						ID:      profile.id,
						Code:    "run_profile_environment_reference_invalid",
						Message: fmt.Sprintf("%s verification environment %s does not exist", label, envID),
					})
				}
			}
		}
	}
	return violations, nil
}

func (db *DB) environmentExists(ctx context.Context, projectID string, envID string) (bool, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_environments WHERE project_id = ? AND id = ?", projectID, envID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func decodeJSONStringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("item %d is empty", i)
		}
	}
	return values, nil
}

func (db *DB) checkTaskVerificationCommands(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, verification_commands_json
FROM tasks
WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	type taskCommands struct {
		taskID string
		raw    string
	}
	var tasks []taskCommands
	for rows.Next() {
		var task taskCommands
		if err := rows.Scan(&task.taskID, &task.raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var violations []InvariantViolation
	for _, task := range tasks {
		raw := strings.TrimSpace(task.raw)
		if raw == "" {
			raw = "[]"
		}
		var commands []TaskVerificationCommand
		if err := json.Unmarshal([]byte(raw), &commands); err != nil {
			violations = append(violations, InvariantViolation{
				Scope:   "task",
				ID:      task.taskID,
				Code:    "task_verification_commands_json_invalid",
				Message: err.Error(),
			})
			continue
		}
		for i, command := range commands {
			if strings.TrimSpace(command.ID) == "" {
				violations = append(violations, InvariantViolation{
					Scope:   "task",
					ID:      task.taskID,
					Code:    "task_verification_command_invalid",
					Message: fmt.Sprintf("verification command %d requires id", i),
				})
			}
			if len(command.Command.Argv) == 0 || strings.TrimSpace(command.Command.Argv[0]) == "" {
				violations = append(violations, InvariantViolation{
					Scope:   "task",
					ID:      task.taskID,
					Code:    "task_verification_command_invalid",
					Message: fmt.Sprintf("verification command %s requires argv", command.ID),
				})
			}
			if command.Timeout != "" {
				if _, err := time.ParseDuration(command.Timeout); err != nil {
					violations = append(violations, InvariantViolation{
						Scope:   "task",
						ID:      task.taskID,
						Code:    "task_verification_command_invalid",
						Message: fmt.Sprintf("verification command %s has invalid timeout: %v", command.ID, err),
					})
				}
			}
			envID := strings.TrimSpace(command.Environment)
			if envID == "" || envID == "primary" {
				continue
			}
			if ok, err := db.environmentExists(ctx, projectID, envID); err != nil {
				return nil, err
			} else if !ok {
				violations = append(violations, InvariantViolation{
					Scope:   "task",
					ID:      task.taskID,
					Code:    "task_verification_environment_invalid",
					Message: fmt.Sprintf("verification command %s references missing environment %s", command.ID, envID),
				})
			}
		}
	}
	return violations, nil
}

func (db *DB) checkApprovedVersionReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, a.approved_version_id, COALESCE(av.artifact_id, '')
FROM artifacts a
LEFT JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND a.approved_version_id IS NOT NULL
  AND (av.id IS NULL OR av.artifact_id != a.id)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, versionArtifactID string
		if err := rows.Scan(&artifactID, &versionID, &versionArtifactID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "approved_version_reference_invalid",
			Message: fmt.Sprintf("approved_version_id %s belongs to %s", versionID, versionArtifactID),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkLatestVersionReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, a.latest_version_id, COALESCE(av.artifact_id, '')
FROM artifacts a
LEFT JOIN artifact_versions av ON av.id = a.latest_version_id
WHERE a.project_id = ?
  AND a.latest_version_id IS NOT NULL
  AND (av.id IS NULL OR av.artifact_id != a.id)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, versionArtifactID string
		if err := rows.Scan(&artifactID, &versionID, &versionArtifactID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "latest_version_reference_invalid",
			Message: fmt.Sprintf("latest_version_id %s belongs to %s", versionID, versionArtifactID),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkApprovedVersionStatuses(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT a.id, av.id, av.status
FROM artifacts a
JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND av.status NOT IN ('approved', 'approved_with_notes')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, versionID, status string
		if err := rows.Scan(&artifactID, &versionID, &status); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact",
			ID:      artifactID,
			Code:    "approved_version_status_invalid",
			Message: fmt.Sprintf("approved_version_id %s has status %s", versionID, status),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkApprovedWithNotes(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT av.id
FROM artifact_versions av
JOIN artifacts a ON a.id = av.artifact_id
WHERE a.project_id = ?
  AND av.status = 'approved_with_notes'
  AND TRIM(COALESCE(av.approval_notes, '')) = ''`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var versionID string
		if err := rows.Scan(&versionID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "artifact_version",
			ID:      versionID,
			Code:    "approved_with_notes_missing_notes",
			Message: "approved_with_notes artifact version requires approval_notes",
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkReadyTaskArtifactContext(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT t.id
FROM tasks t
WHERE t.project_id = ?
  AND t.status = 'ready'
  AND NOT (
    EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'prd' AND av.status IN ('approved', 'approved_with_notes'))
    AND EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'architecture' AND av.status IN ('approved', 'approved_with_notes'))
    AND EXISTS (SELECT 1 FROM artifacts a JOIN artifact_versions av ON av.id = a.approved_version_id WHERE a.project_id = t.project_id AND a.artifact_type = 'roadmap' AND av.status IN ('approved', 'approved_with_notes'))
  )`, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "task",
			ID:      taskID,
			Code:    "ready_task_missing_trusted_artifacts",
			Message: "ready task requires approved PRD, architecture, and roadmap artifacts",
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkTaskCurrentRunReferences(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT t.id, t.current_run_id
FROM tasks t
LEFT JOIN runs r ON r.project_id = t.project_id AND r.task_id = t.id AND r.id = t.current_run_id
WHERE t.project_id = ?
  AND t.current_run_id IS NOT NULL
  AND r.id IS NULL`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var taskID, runID string
		if err := rows.Scan(&taskID, &runID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "task",
			ID:      taskID,
			Code:    "current_run_reference_invalid",
			Message: fmt.Sprintf("current_run_id %s does not point to a run for the same task/project", runID),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkConcurrentRunningRuns(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT task_id, run_type, COUNT(*)
FROM runs
WHERE project_id = ? AND task_id IS NOT NULL AND status = 'running'
GROUP BY task_id, run_type
HAVING COUNT(*) > 1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var taskID, runType string
		var count int
		if err := rows.Scan(&taskID, &runType, &count); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "task",
			ID:      taskID,
			Code:    "concurrent_running_runs",
			Message: fmt.Sprintf("%d running %s runs exist for one task", count, runType),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkVerificationRunTypes(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT vr.id, r.run_type
FROM verification_results vr
JOIN runs r ON r.project_id = vr.project_id AND r.id = vr.run_id
WHERE vr.project_id = ?
  AND r.run_type NOT IN ('verification', 'reverify')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var resultID, runType string
		if err := rows.Scan(&resultID, &runType); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "verification_result",
			ID:      resultID,
			Code:    "verification_result_run_type_invalid",
			Message: fmt.Sprintf("verification result belongs to %s run", runType),
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkRequiredVerificationFailuresClassified(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id
FROM verification_results
WHERE project_id = ?
  AND required_for_merge = 1
  AND status IN ('failed', 'error')
  AND failure_class IS NULL`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var resultID string
		if err := rows.Scan(&resultID); err != nil {
			return nil, err
		}
		violations = append(violations, InvariantViolation{
			Scope:   "verification_result",
			ID:      resultID,
			Code:    "required_verification_failure_unclassified",
			Message: "required-for-merge verification failure requires failure_class",
		})
	}
	return violations, rows.Err()
}

func (db *DB) checkRunArtifactFiles(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, path, content_hash
FROM run_artifacts
WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []InvariantViolation
	for rows.Next() {
		var artifactID, relPath, contentHash string
		if err := rows.Scan(&artifactID, &relPath, &contentHash); err != nil {
			return nil, err
		}
		cleanPath := filepath.Clean(relPath)
		if filepath.IsAbs(relPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
			violations = append(violations, InvariantViolation{
				Scope:   "run_artifact",
				ID:      artifactID,
				Code:    "run_artifact_path_escapes_data_root",
				Message: fmt.Sprintf("run artifact path escapes data root: %s", relPath),
			})
			continue
		}
		content, err := os.ReadFile(filepath.Join(db.dataRoot, cleanPath))
		if err != nil {
			violations = append(violations, InvariantViolation{
				Scope:   "run_artifact",
				ID:      artifactID,
				Code:    "run_artifact_file_missing",
				Message: fmt.Sprintf("run artifact file cannot be read: %v", err),
			})
			continue
		}
		if got := sha256Hex(content); got != contentHash {
			violations = append(violations, InvariantViolation{
				Scope:   "run_artifact",
				ID:      artifactID,
				Code:    "run_artifact_hash_mismatch",
				Message: fmt.Sprintf("run artifact hash %s does not match stored hash %s", got, contentHash),
			})
		}
	}
	return violations, rows.Err()
}

func (db *DB) checkPathMappingServiceBuilds(ctx context.Context, projectID string) ([]InvariantViolation, error) {
	if _, err := db.BuildPathMappingService(ctx, projectID); err != nil {
		return []InvariantViolation{{
			Scope:   "path_mapping",
			ID:      projectID,
			Code:    "path_mapping_service_invalid",
			Message: err.Error(),
		}}, nil
	}
	return nil, nil
}
