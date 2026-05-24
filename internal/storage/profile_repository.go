package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type RunProfileRecord struct {
	ID                               string                `json:"id"`
	Name                             string                `json:"name"`
	Mode                             platform.PlatformMode `json:"mode"`
	Status                           string                `json:"status"`
	PrimaryEnvironmentID             string                `json:"primary_environment_id"`
	ImplementationEnvironmentID      string                `json:"implementation_environment_id"`
	GitEnvironmentID                 string                `json:"git_environment_id"`
	MergeEnvironmentID               string                `json:"merge_environment_id"`
	RequiredVerificationEnvironments []string              `json:"required_verification_environment_ids"`
	OptionalVerificationEnvironments []string              `json:"optional_verification_environment_ids"`
}

func (db *DB) ConfigureFakeRunProfile(ctx context.Context, projectID string, mode platform.PlatformMode, projectRoot string) (RunProfileRecord, error) {
	if !platform.ValidPlatformMode(mode) {
		return RunProfileRecord{}, fmt.Errorf("invalid platform mode: %s", mode)
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return RunProfileRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE execution_environments SET role = 'sidecar', updated_at = ? WHERE project_id = ? AND role = 'primary'", now, projectID); err != nil {
		return RunProfileRecord{}, err
	}
	profile, envs, err := fakeProfileDefinition(projectID, mode, projectRoot)
	if err != nil {
		return RunProfileRecord{}, err
	}
	for _, env := range envs {
		if err := upsertEnvironment(ctx, tx, projectID, env, now); err != nil {
			return RunProfileRecord{}, err
		}
	}
	requiredJSON, err := json.Marshal(profile.RequiredVerificationEnvironments)
	if err != nil {
		return RunProfileRecord{}, err
	}
	optionalJSON, err := json.Marshal(profile.OptionalVerificationEnvironments)
	if err != nil {
		return RunProfileRecord{}, err
	}
	opsJSON, err := json.Marshal(map[string]any{
		"git_status":     profile.GitEnvironmentID,
		"worktree":       profile.GitEnvironmentID,
		"implementation": profile.ImplementationEnvironmentID,
		"merge":          profile.MergeEnvironmentID,
		"verification": map[string]any{
			"required": profile.RequiredVerificationEnvironments,
			"optional": profile.OptionalVerificationEnvironments,
		},
		"artifact_write": "core",
	})
	if err != nil {
		return RunProfileRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE project_run_profiles SET status = 'disabled', updated_at = ? WHERE project_id = ? AND status = 'active'", now, projectID); err != nil {
		return RunProfileRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO project_run_profiles(
  id, project_id, name, mode, status, primary_environment_id, implementation_environment_id,
  git_environment_id, merge_environment_id, required_verification_environment_ids_json,
  optional_verification_environment_ids_json, canonical_operations_json, created_at, updated_at
) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, name) DO UPDATE SET
  mode = excluded.mode,
  status = 'active',
  primary_environment_id = excluded.primary_environment_id,
  implementation_environment_id = excluded.implementation_environment_id,
  git_environment_id = excluded.git_environment_id,
  merge_environment_id = excluded.merge_environment_id,
  required_verification_environment_ids_json = excluded.required_verification_environment_ids_json,
  optional_verification_environment_ids_json = excluded.optional_verification_environment_ids_json,
  canonical_operations_json = excluded.canonical_operations_json,
  updated_at = excluded.updated_at`,
		profile.ID, projectID, profile.Name, profile.Mode, profile.PrimaryEnvironmentID,
		profile.ImplementationEnvironmentID, profile.GitEnvironmentID, profile.MergeEnvironmentID,
		string(requiredJSON), string(optionalJSON), string(opsJSON), now, now,
	); err != nil {
		return RunProfileRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE projects SET primary_environment_id = ?, updated_at = ? WHERE id = ?", profile.PrimaryEnvironmentID, now, projectID); err != nil {
		return RunProfileRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "run_profile_configured", profile, now); err != nil {
		return RunProfileRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunProfileRecord{}, err
	}
	committed = true
	return profile, nil
}

func (db *DB) ListRunProfiles(ctx context.Context, projectID string) ([]RunProfileRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, name, mode, status, primary_environment_id, implementation_environment_id,
       git_environment_id, merge_environment_id,
       required_verification_environment_ids_json,
       optional_verification_environment_ids_json
FROM project_run_profiles
WHERE project_id = ?
ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []RunProfileRecord
	for rows.Next() {
		var profile RunProfileRecord
		var requiredJSON, optionalJSON string
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Mode, &profile.Status,
			&profile.PrimaryEnvironmentID, &profile.ImplementationEnvironmentID,
			&profile.GitEnvironmentID, &profile.MergeEnvironmentID,
			&requiredJSON, &optionalJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(requiredJSON), &profile.RequiredVerificationEnvironments); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(optionalJSON), &profile.OptionalVerificationEnvironments); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func fakeProfileDefinition(projectID string, mode platform.PlatformMode, projectRoot string) (RunProfileRecord, []platform.ExecutionEnvironment, error) {
	if strings.TrimSpace(projectRoot) == "" {
		projectRoot = "/repo"
	}
	switch mode {
	case platform.PlatformModeWindowsPrimary:
		env := fakeWindowsEnvironment("windows-main", platform.RolePrimary, projectRoot)
		return fakeProfile(projectID, "windows-primary", mode, env.ID, []string{env.ID}, nil), []platform.ExecutionEnvironment{env}, nil
	case platform.PlatformModeWSLPrimary:
		env := fakeWSLEnvironment("wsl-main", platform.RolePrimary, projectRoot)
		return fakeProfile(projectID, "wsl-primary", mode, env.ID, []string{env.ID}, nil), []platform.ExecutionEnvironment{env}, nil
	case platform.PlatformModeHybrid:
		primary := fakeWindowsEnvironment("windows-main", platform.RolePrimary, projectRoot)
		sidecar := fakeWSLEnvironment("wsl-sidecar", platform.RoleSidecar, projectRoot)
		return fakeProfile(projectID, "hybrid", mode, primary.ID, []string{primary.ID}, []string{sidecar.ID}), []platform.ExecutionEnvironment{primary, sidecar}, nil
	case platform.PlatformModeSingleEnvironment:
		env := fakeLinuxEnvironment("linux-main", platform.RolePrimary, projectRoot)
		return fakeProfile(projectID, "single-environment", mode, env.ID, []string{env.ID}, nil), []platform.ExecutionEnvironment{env}, nil
	default:
		return RunProfileRecord{}, nil, fmt.Errorf("unsupported profile mode: %s", mode)
	}
}

func fakeProfile(projectID string, name string, mode platform.PlatformMode, primaryEnvID string, required []string, optional []string) RunProfileRecord {
	return RunProfileRecord{
		ID:                               "RUNPROFILE-" + stableShortHash(projectID+"|"+name),
		Name:                             name,
		Mode:                             mode,
		Status:                           "active",
		PrimaryEnvironmentID:             primaryEnvID,
		ImplementationEnvironmentID:      primaryEnvID,
		GitEnvironmentID:                 primaryEnvID,
		MergeEnvironmentID:               primaryEnvID,
		RequiredVerificationEnvironments: required,
		OptionalVerificationEnvironments: optional,
	}
}

func fakeWindowsEnvironment(id string, role platform.Role, projectRoot string) platform.ExecutionEnvironment {
	if !looksLikeWindowsRoot(projectRoot) {
		projectRoot = `C:\fake\project`
	}
	return platform.ExecutionEnvironment{
		ID:             id,
		OSFamily:       platform.OSFamilyWindows,
		Role:           role,
		Shell:          platform.ShellPowerShell,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderWindows,
		CodexAdapter:   platform.CodexAdapterWindows,
		SandboxProfile: platform.SandboxWindowsNative,
		Status:         "configured",
	}
}

func looksLikeWindowsRoot(path string) bool {
	trimmed := strings.TrimSpace(path)
	if len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/') {
		return true
	}
	return strings.HasPrefix(trimmed, `\\`)
}

func fakeWSLEnvironment(id string, role platform.Role, projectRoot string) platform.ExecutionEnvironment {
	return platform.ExecutionEnvironment{
		ID:             id,
		OSFamily:       platform.OSFamilyWSL,
		Role:           role,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterWSL,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	}
}

func fakeLinuxEnvironment(id string, role platform.Role, projectRoot string) platform.ExecutionEnvironment {
	return platform.ExecutionEnvironment{
		ID:             id,
		OSFamily:       platform.OSFamilyLinux,
		Role:           role,
		Shell:          platform.ShellBash,
		ProjectRoot:    projectRoot,
		GitProvider:    platform.GitProviderLinux,
		CodexAdapter:   platform.CodexAdapterLinux,
		SandboxProfile: platform.SandboxLinuxBubblewrap,
		Status:         "configured",
	}
}
