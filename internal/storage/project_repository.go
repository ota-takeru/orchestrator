package storage

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/preflight"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

type ProjectInitInput struct {
	Name            string
	RootPath        string
	Environment     platform.ExecutionEnvironment
	PreflightReport preflight.Report
	ToolchainReport *toolchains.Report
}

type ProjectRecord struct {
	ID                   string
	PrimaryEnvironmentID string
	Created              bool
}

func (db *DB) SaveProjectInit(ctx context.Context, input ProjectInitInput) (ProjectRecord, error) {
	if strings.TrimSpace(input.RootPath) == "" {
		return ProjectRecord{}, fmt.Errorf("root path is required")
	}
	if strings.TrimSpace(input.Environment.ID) == "" {
		return ProjectRecord{}, fmt.Errorf("environment id is required")
	}
	projectID := ProjectIDForRoot(input.RootPath)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(input.RootPath)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ProjectRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&exists); err != nil {
		return ProjectRecord{}, err
	}
	created := exists == 0
	if created {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO projects(
  id, name, root_path, lifecycle_status, archive_status, primary_environment_id, created_at, updated_at
) VALUES (?, ?, ?, 'concept', 'active', ?, ?, ?)`,
			projectID, name, input.RootPath, input.Environment.ID, now, now,
		); err != nil {
			return ProjectRecord{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
UPDATE projects
SET name = ?, root_path = ?, primary_environment_id = ?, updated_at = ?
WHERE id = ?`,
			name, input.RootPath, input.Environment.ID, now, projectID,
		); err != nil {
			return ProjectRecord{}, err
		}
	}

	if err := upsertEnvironment(ctx, tx, projectID, input.Environment, now); err != nil {
		return ProjectRecord{}, err
	}
	if err := syncPreflightInboxItems(ctx, tx, projectID, input.Environment.ID, input.PreflightReport, now); err != nil {
		return ProjectRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "preflight_report_captured", input.PreflightReport, now); err != nil {
		return ProjectRecord{}, err
	}
	if input.ToolchainReport != nil {
		if err := insertWorkflowEvent(ctx, tx, projectID, "toolchain_report_captured", input.ToolchainReport, now); err != nil {
			return ProjectRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProjectRecord{}, err
	}
	committed = true
	return ProjectRecord{ID: projectID, PrimaryEnvironmentID: input.Environment.ID, Created: created}, nil
}

func ProjectIDForRoot(root string) string {
	normalized := filepath.ToSlash(filepath.Clean(root))
	sum := sha1.Sum([]byte(normalized))
	return "PROJECT-" + strings.ToUpper(hex.EncodeToString(sum[:])[:12])
}

func upsertEnvironment(ctx context.Context, tx *sql.Tx, projectID string, env platform.ExecutionEnvironment, now string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO execution_environments(
  id, project_id, os_family, role, shell, project_root, git_provider,
  codex_adapter, sandbox_profile, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  project_id = excluded.project_id,
  os_family = excluded.os_family,
  role = excluded.role,
  shell = excluded.shell,
  project_root = excluded.project_root,
  git_provider = excluded.git_provider,
  codex_adapter = excluded.codex_adapter,
  sandbox_profile = excluded.sandbox_profile,
  status = excluded.status,
  updated_at = excluded.updated_at`,
		env.ID, projectID, env.OSFamily, env.Role, env.Shell, env.ProjectRoot,
		env.GitProvider, env.CodexAdapter, env.SandboxProfile, env.Status, now, now,
	)
	return err
}

func syncPreflightInboxItems(ctx context.Context, tx *sql.Tx, projectID string, environmentID string, report preflight.Report, now string) error {
	for _, finding := range report.Findings {
		dedupeKey := preflightInboxDedupeKey(projectID, environmentID, finding.ID)
		switch finding.Severity {
		case preflight.SeverityWarn, preflight.SeverityBlock:
			if err := upsertPreflightInboxItem(ctx, tx, projectID, environmentID, dedupeKey, finding, now); err != nil {
				return err
			}
		case preflight.SeverityPass:
			if _, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND dedupe_key = ? AND status = 'open'`,
				now, now, projectID, dedupeKey,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertPreflightInboxItem(ctx context.Context, tx *sql.Tx, projectID string, environmentID string, dedupeKey string, finding preflight.Finding, now string) error {
	itemID := "INBOX-" + stableShortHash(dedupeKey)
	body := finding.Message
	if len(finding.Details) > 0 {
		body += "\n" + strings.Join(finding.Details, "\n")
	}
	title := "Platform setup required: " + finding.ID
	_, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, item_type, status, source_type, source_id, dedupe_key,
  batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, 'platform_setup', 'open', 'execution_environment', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = 'open',
  title = excluded.title,
  body = excluded.body,
  priority = excluded.priority,
  resolved_at = NULL,
  updated_at = excluded.updated_at`,
		itemID, projectID, environmentID, dedupeKey,
		projectID+":platform_setup:"+environmentID,
		preflightInboxPriority(finding.Severity), title, body, now, now,
	)
	return err
}

func preflightInboxDedupeKey(projectID string, environmentID string, findingID string) string {
	return strings.Join([]string{projectID, environmentID, "platform_setup", findingID}, ":")
}

func preflightInboxPriority(severity preflight.Severity) int {
	if severity == preflight.SeverityBlock {
		return 90
	}
	return 30
}

func insertWorkflowEvent(ctx context.Context, tx *sql.Tx, projectID string, eventType string, evidence any, now string) error {
	payload, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	eventID := workflowEventID(projectID, eventType, payload, now)
	_, err = tx.ExecContext(ctx, `
INSERT INTO workflow_events(
  id, project_id, event_type, evidence_json, created_at
) VALUES (?, ?, ?, ?, ?)`,
		eventID, projectID, eventType, string(payload), now,
	)
	return err
}

func workflowEventID(projectID string, eventType string, payload []byte, now string) string {
	sum := sha1.Sum(append([]byte(projectID+"|"+eventType+"|"+now+"|"), payload...))
	return "WFEVT-" + strings.ToUpper(hex.EncodeToString(sum[:])[:16])
}
