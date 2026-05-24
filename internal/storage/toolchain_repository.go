package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

type ToolchainSetupInstructions struct {
	InboxID          string            `json:"inbox_id"`
	RequirementID    string            `json:"requirement_id"`
	EnvironmentID    string            `json:"environment_id"`
	OSFamily         platform.OSFamily `json:"os_family"`
	ToolchainKey     string            `json:"toolchain_key"`
	RequiredFor      string            `json:"required_for"`
	RequiredForMerge bool              `json:"required_for_merge"`
	Status           string            `json:"status"`
	Message          string            `json:"message"`
	Instructions     []string          `json:"instructions"`
	RerunCommand     string            `json:"rerun_command"`
}

type ToolchainWaiverInput struct {
	ProjectID     string
	InboxID       string
	Reason        string
	Scope         string
	Expiry        string
	AllowedEffect string
}

type ToolchainWaiverRecord struct {
	DecisionID     string `json:"decision_id"`
	RequirementID  string `json:"requirement_id"`
	InboxID        string `json:"inbox_id"`
	Status         string `json:"status"`
	AllowedEffect  string `json:"allowed_effect"`
	RequirementKey string `json:"toolchain_key"`
}

type toolchainWaiverEvidence struct {
	InboxID        string `json:"inbox_id"`
	RequirementID  string `json:"requirement_id"`
	EnvironmentID  string `json:"environment_id"`
	ToolchainKey   string `json:"toolchain_key"`
	Reason         string `json:"reason"`
	Scope          string `json:"scope"`
	Expiry         string `json:"expiry"`
	AllowedEffect  string `json:"allowed_effect"`
	PreviousStatus string `json:"previous_status"`
}

func (db *DB) SaveToolchainReport(ctx context.Context, projectID string, report toolchains.Report) error {
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(report.EnvironmentID) == "" {
		return fmt.Errorf("environment id is required")
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
	for _, requirement := range report.Requirements {
		requirementID := toolchainRequirementID(projectID, report.EnvironmentID, requirement)
		if err := upsertToolchainRequirement(ctx, tx, projectID, report.EnvironmentID, requirementID, requirement, now); err != nil {
			return err
		}
		if requiresSetupCard(requirement.Status) {
			if err := upsertToolchainInboxItem(ctx, tx, projectID, requirementID, report.EnvironmentID, requirement, now); err != nil {
				return err
			}
		} else if err := resolveToolchainInboxItem(ctx, tx, projectID, requirementID, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func upsertToolchainRequirement(ctx context.Context, tx *sql.Tx, projectID string, environmentID string, requirementID string, requirement toolchains.Requirement, now string) error {
	evidence, err := json.Marshal(map[string]any{
		"executable":    requirement.Executable,
		"detected_path": requirement.DetectedPath,
		"message":       requirement.Message,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO toolchain_requirements(
  id, project_id, environment_id, toolchain_key, required_for, required_for_merge,
  status, detected_version, required_version, evidence_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = excluded.status,
  evidence_json = excluded.evidence_json,
  updated_at = excluded.updated_at`,
		requirementID, projectID, environmentID, requirement.ToolchainKey, requirement.RequiredFor,
		boolInt(requirement.RequiredForMerge), requirement.Status, string(evidence), now, now,
	)
	return err
}

func upsertToolchainInboxItem(ctx context.Context, tx *sql.Tx, projectID string, requirementID string, environmentID string, requirement toolchains.Requirement, now string) error {
	dedupeKey := strings.Join([]string{projectID, environmentID, "toolchain_setup", requirementID}, ":")
	itemID := "INBOX-" + stableShortHash(dedupeKey)
	title := fmt.Sprintf("Toolchain setup required: %s", requirement.ToolchainKey)
	body := fmt.Sprintf("%s\nEnvironment: %s", requirement.Message, environmentID)
	_, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, 'toolchain_setup', 'open', 'toolchain_requirement', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  updated_at = excluded.updated_at`,
		itemID, projectID, requirementID, dedupeKey,
		projectID+":toolchain_setup:"+requirement.ToolchainKey,
		toolchainPriority(requirement), title, body, now, now,
	)
	return err
}

func resolveToolchainInboxItem(ctx context.Context, tx *sql.Tx, projectID string, requirementID string, now string) error {
	_, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND source_type = 'toolchain_requirement' AND source_id = ? AND status = 'open'`,
		now, now, projectID, requirementID,
	)
	return err
}

func (db *DB) ToolchainSetupInstructions(ctx context.Context, projectID string, inboxID string) (ToolchainSetupInstructions, error) {
	if strings.TrimSpace(projectID) == "" {
		return ToolchainSetupInstructions{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(inboxID) == "" {
		return ToolchainSetupInstructions{}, fmt.Errorf("inbox id is required")
	}
	var out ToolchainSetupInstructions
	var evidenceJSON string
	var requiredForMerge int
	if err := db.sql.QueryRowContext(ctx, `
SELECT ii.id, tr.id, tr.environment_id, ee.os_family, tr.toolchain_key,
       tr.required_for, tr.required_for_merge, tr.status, tr.evidence_json
FROM inbox_items ii
JOIN toolchain_requirements tr ON tr.project_id = ii.project_id AND tr.id = ii.source_id
JOIN execution_environments ee ON ee.project_id = tr.project_id AND ee.id = tr.environment_id
WHERE ii.project_id = ? AND ii.id = ? AND ii.source_type = 'toolchain_requirement'`,
		projectID, inboxID,
	).Scan(
		&out.InboxID,
		&out.RequirementID,
		&out.EnvironmentID,
		&out.OSFamily,
		&out.ToolchainKey,
		&out.RequiredFor,
		&requiredForMerge,
		&out.Status,
		&evidenceJSON,
	); err != nil {
		if err == sql.ErrNoRows {
			return ToolchainSetupInstructions{}, fmt.Errorf("toolchain setup inbox item not found: %s", inboxID)
		}
		return ToolchainSetupInstructions{}, err
	}
	out.RequiredForMerge = requiredForMerge == 1
	var evidence struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err == nil {
		out.Message = evidence.Message
	}
	out.Instructions = setupInstructionsFor(out.OSFamily, out.ToolchainKey)
	out.RerunCommand = "devos platform doctor --save --project-root <PROJECT_ROOT>"
	return out, nil
}

func (db *DB) WaiveToolchainRequirement(ctx context.Context, input ToolchainWaiverInput) (ToolchainWaiverRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ToolchainWaiverRecord{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.InboxID) == "" {
		return ToolchainWaiverRecord{}, fmt.Errorf("inbox id is required")
	}
	reason := strings.TrimSpace(input.Reason)
	scope := strings.TrimSpace(input.Scope)
	expiry := strings.TrimSpace(input.Expiry)
	allowedEffect := strings.TrimSpace(input.AllowedEffect)
	if reason == "" || scope == "" || expiry == "" || allowedEffect == "" {
		return ToolchainWaiverRecord{}, fmt.Errorf("reason, scope, expiry, and allowed-effect are required")
	}
	if _, err := time.Parse(time.RFC3339, expiry); err != nil {
		return ToolchainWaiverRecord{}, fmt.Errorf("expiry must be RFC3339: %w", err)
	}
	if !validToolchainWaiverEffect(allowedEffect) {
		return ToolchainWaiverRecord{}, fmt.Errorf("invalid allowed-effect: %s", allowedEffect)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ToolchainWaiverRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var requirementID, environmentID, toolchainKey, currentStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT tr.id, tr.environment_id, tr.toolchain_key, tr.status
FROM inbox_items ii
JOIN toolchain_requirements tr ON tr.project_id = ii.project_id AND tr.id = ii.source_id
WHERE ii.project_id = ? AND ii.id = ? AND ii.source_type = 'toolchain_requirement' AND ii.status = 'open'`,
		input.ProjectID, input.InboxID,
	).Scan(&requirementID, &environmentID, &toolchainKey, &currentStatus); err != nil {
		if err == sql.ErrNoRows {
			return ToolchainWaiverRecord{}, fmt.Errorf("open toolchain setup inbox item not found: %s", input.InboxID)
		}
		return ToolchainWaiverRecord{}, err
	}
	if err := statemachine.ToolchainRequirement.ValidateTransition(currentStatus, "waived"); err != nil {
		return ToolchainWaiverRecord{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	decisionID := "DEC-" + stableShortHash(input.ProjectID+"|toolchain-waiver|"+requirementID+"|"+now)
	options, err := json.Marshal([]map[string]string{
		{"id": allowedEffect, "label": "Waive toolchain requirement"},
	})
	if err != nil {
		return ToolchainWaiverRecord{}, err
	}
	evidence, err := json.Marshal(toolchainWaiverEvidence{
		InboxID:        input.InboxID,
		RequirementID:  requirementID,
		EnvironmentID:  environmentID,
		ToolchainKey:   toolchainKey,
		Reason:         reason,
		Scope:          scope,
		Expiry:         expiry,
		AllowedEffect:  allowedEffect,
		PreviousStatus: currentStatus,
	})
	if err != nil {
		return ToolchainWaiverRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO decisions(
  id, project_id, status, title, options_json, selected_option, evidence_json,
  created_at, updated_at, resolved_at
) VALUES (?, ?, 'approved', 'Toolchain requirement waived', ?, ?, ?, ?, ?, ?)`,
		decisionID, input.ProjectID, string(options), allowedEffect, string(evidence), now, now, now,
	); err != nil {
		return ToolchainWaiverRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE toolchain_requirements
SET status = 'waived', evidence_json = ?, updated_at = ?
WHERE project_id = ? AND id = ?`,
		string(evidence), now, input.ProjectID, requirementID,
	); err != nil {
		return ToolchainWaiverRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND id = ? AND status = 'open'`,
		now, now, input.ProjectID, input.InboxID,
	); err != nil {
		return ToolchainWaiverRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "toolchain_requirement_waived", map[string]any{
		"decision_id":    decisionID,
		"requirement_id": requirementID,
		"inbox_id":       input.InboxID,
		"allowed_effect": allowedEffect,
	}, now); err != nil {
		return ToolchainWaiverRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ToolchainWaiverRecord{}, err
	}
	committed = true
	return ToolchainWaiverRecord{
		DecisionID:     decisionID,
		RequirementID:  requirementID,
		InboxID:        input.InboxID,
		Status:         "waived",
		AllowedEffect:  allowedEffect,
		RequirementKey: toolchainKey,
	}, nil
}

func (db *DB) RevokeExpiredToolchainWaivers(ctx context.Context, projectID string, nowTime time.Time) error {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, environment_id, toolchain_key, evidence_json
FROM toolchain_requirements
WHERE project_id = ? AND status = 'waived'`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type expiredWaiver struct {
		requirementID string
		environmentID string
		toolchainKey  string
		evidence      toolchainWaiverEvidence
	}
	var expired []expiredWaiver
	for rows.Next() {
		var item expiredWaiver
		var evidenceJSON string
		if err := rows.Scan(&item.requirementID, &item.environmentID, &item.toolchainKey, &evidenceJSON); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &item.evidence); err != nil {
			continue
		}
		expiry, err := time.Parse(time.RFC3339, item.evidence.Expiry)
		if err != nil {
			continue
		}
		if !nowTime.Before(expiry) {
			expired = append(expired, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
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
	now := nowTime.UTC().Format(time.RFC3339Nano)
	for _, item := range expired {
		if err := statemachine.ToolchainRequirement.ValidateTransition("waived", "revoked"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE toolchain_requirements
SET status = 'revoked', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'waived'`,
			now, projectID, item.requirementID,
		); err != nil {
			return err
		}
		dedupeKey := strings.Join([]string{projectID, item.environmentID, "toolchain_setup", item.requirementID}, ":")
		inboxID := "INBOX-" + stableShortHash(dedupeKey+"|waiver-expired|"+now)
		title := fmt.Sprintf("Toolchain setup required: %s", item.toolchainKey)
		body := fmt.Sprintf("Toolchain waiver expired at %s\nEnvironment: %s", item.evidence.Expiry, item.environmentID)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, 'toolchain_setup', 'open', 'toolchain_requirement', ?, ?, ?, 80, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  priority = excluded.priority,
  updated_at = excluded.updated_at`,
			inboxID, projectID, item.requirementID, dedupeKey,
			projectID+":toolchain_setup:"+item.toolchainKey, title, body, now, now,
		); err != nil {
			return err
		}
		if err := insertWorkflowEvent(ctx, tx, projectID, "toolchain_waiver_expired", map[string]any{
			"requirement_id": item.requirementID,
			"environment_id": item.environmentID,
			"toolchain_key":  item.toolchainKey,
			"expiry":         item.evidence.Expiry,
		}, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func setupInstructionsFor(osFamily platform.OSFamily, toolchainKey string) []string {
	switch toolchainKey {
	case "codex-auth":
		return []string{
			"Authenticate Codex manually in this execution environment.",
			"Do not copy or share CODEX_HOME between Windows and WSL environments.",
			"Rerun platform doctor with --save after setup.",
		}
	case "codex":
		return []string{
			"Install or update the Codex CLI manually using the official OpenAI instructions.",
			"Authenticate Codex in the existing CODEX_HOME for this environment.",
			"Rerun platform doctor with --save after setup.",
		}
	case "git":
		if osFamily == platform.OSFamilyWindows || osFamily == platform.OSFamilyRemoteWindows {
			return []string{
				"Install Git for Windows manually.",
				"Make git available on PATH for the configured shell.",
				"Rerun platform doctor with --save after setup.",
			}
		}
		return []string{
			"Install git with the operating system package manager or approved manual process.",
			"Make git available on PATH for this environment.",
			"Rerun platform doctor with --save after setup.",
		}
	case "bubblewrap":
		return []string{
			"Install bubblewrap so the bwrap executable is available on PATH.",
			"Do not run package manager commands through DevOS; perform setup manually.",
			"Rerun platform doctor with --save after setup.",
		}
	case "node":
		return []string{
			"Install Node.js manually for this execution environment.",
			"Make node available on PATH for the configured shell.",
			"Rerun platform doctor with --include-ui --save after setup.",
		}
	case "corepack":
		return []string{
			"Install or enable Corepack for the Node.js runtime in this execution environment.",
			"Use Corepack to run the repository-pinned pnpm version, for example: corepack pnpm --dir ui test.",
			"Rerun platform doctor with --include-ui --save after setup.",
		}
	case "wsl2":
		return []string{
			"Upgrade or recreate this Linux environment as WSL2 before using the Codex WSL adapter.",
			"Verify the environment from the target WSL shell, not from Windows PowerShell.",
			"Rerun platform doctor with --save after setup.",
		}
	case "bash", "sh", "powershell", "cmd":
		return []string{
			"Install or enable the required shell for this execution environment.",
			"Make the shell executable available on PATH.",
			"Rerun platform doctor with --save after setup.",
		}
	default:
		return []string{
			"Install the missing toolchain manually using the project's approved setup process.",
			"Make the required executable available on PATH for this environment.",
			"Rerun platform doctor with --save after setup.",
		}
	}
}

func validToolchainWaiverEffect(effect string) bool {
	switch effect {
	case "report_only", "allow_non_merge_without_toolchain", "allow_merge_without_toolchain":
		return true
	default:
		return false
	}
}

func requiresSetupCard(status toolchains.Status) bool {
	switch status {
	case toolchains.StatusMissing, toolchains.StatusInvalid, toolchains.StatusSetupRequired, toolchains.StatusUnsupported:
		return true
	default:
		return false
	}
}

func toolchainPriority(requirement toolchains.Requirement) int {
	if requirement.RequiredForMerge {
		return 80
	}
	return 40
}

func toolchainRequirementID(projectID string, environmentID string, requirement toolchains.Requirement) string {
	return "TOOLREQ-" + stableShortHash(projectID+"|"+environmentID+"|"+requirement.ToolchainKey+"|"+string(requirement.RequiredFor))
}
