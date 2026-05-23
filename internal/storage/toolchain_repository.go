package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

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
