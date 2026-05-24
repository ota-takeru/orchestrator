package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type DependencyRiskRecord struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	Name               string `json:"name"`
	PackageManager     string `json:"package_manager"`
	DependencyType     string `json:"dependency_type"`
	IntroducedByTaskID string `json:"introduced_by_task_id,omitempty"`
	IntroducedByRunID  string `json:"introduced_by_run_id,omitempty"`
	DecisionID         string `json:"decision_id,omitempty"`
	Reason             string `json:"reason"`
	ApprovedBy         string `json:"approved_by,omitempty"`
	Risk               string `json:"risk"`
	LockfileChanged    bool   `json:"lockfile_changed"`
	LifecycleScripts   string `json:"lifecycle_scripts"`
	CurrentVersion     string `json:"current_version,omitempty"`
	ApprovedScope      string `json:"approved_scope"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type DependencyRiskInput struct {
	ProjectID          string
	Name               string
	PackageManager     string
	DependencyType     string
	IntroducedByTaskID string
	IntroducedByRunID  string
	DecisionID         string
	Reason             string
	ApprovedBy         string
	Risk               string
	LockfileChanged    bool
	LifecycleScripts   string
	CurrentVersion     string
	ApprovedScope      string
	ExpiresAt          string
}

type DependencyRiskListFilter struct {
	ProjectID      string
	PackageManager string
	DependencyType string
	Risk           string
}

func (db *DB) RecordDependencyRisk(ctx context.Context, input DependencyRiskInput) (DependencyRiskRecord, error) {
	record, err := normalizeDependencyRiskInput(input)
	if err != nil {
		return DependencyRiskRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record.ID = "DEPRISK-" + stableShortHash(strings.Join([]string{
		record.ProjectID,
		record.PackageManager,
		record.Name,
		record.DependencyType,
		record.CurrentVersion,
		now,
	}, "|"))
	record.CreatedAt = now
	record.UpdatedAt = now

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return DependencyRiskRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `
INSERT INTO dependency_risk_ledger(
  id, project_id, name, package_manager, dependency_type, introduced_by_task_id,
  introduced_by_run_id, decision_id, reason, approved_by, risk, lockfile_changed,
  lifecycle_scripts, current_version, approved_scope, expires_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, record.Name, record.PackageManager, record.DependencyType,
		nullableText(record.IntroducedByTaskID), nullableText(record.IntroducedByRunID),
		nullableText(record.DecisionID), record.Reason, nullableText(record.ApprovedBy), record.Risk,
		boolInt(record.LockfileChanged), record.LifecycleScripts, nullableText(record.CurrentVersion),
		record.ApprovedScope, nullableText(record.ExpiresAt), record.CreatedAt, record.UpdatedAt,
	)
	if err != nil {
		return DependencyRiskRecord{}, err
	}
	if err := insertWorkflowEvent(ctx, tx, record.ProjectID, "dependency_risk_recorded", map[string]any{
		"dependency_risk_id": record.ID,
		"name":               record.Name,
		"package_manager":    record.PackageManager,
		"dependency_type":    record.DependencyType,
		"risk":               record.Risk,
		"approved_scope":     record.ApprovedScope,
	}, now); err != nil {
		return DependencyRiskRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return DependencyRiskRecord{}, err
	}
	committed = true
	return record, nil
}

func (db *DB) ListDependencyRisks(ctx context.Context, filter DependencyRiskListFilter) ([]DependencyRiskRecord, error) {
	projectID := strings.TrimSpace(filter.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	query := `
SELECT id, project_id, name, package_manager, dependency_type, introduced_by_task_id,
       introduced_by_run_id, decision_id, reason, approved_by, risk, lockfile_changed,
       lifecycle_scripts, current_version, approved_scope, expires_at, created_at, updated_at
FROM dependency_risk_ledger
WHERE project_id = ?`
	args := []any{projectID}
	if value := strings.TrimSpace(filter.PackageManager); value != "" {
		if !validDependencyPackageManager(value) {
			return nil, fmt.Errorf("invalid package manager: %s", value)
		}
		query += " AND package_manager = ?"
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.DependencyType); value != "" {
		if !validLedgerDependencyType(value) {
			return nil, fmt.Errorf("invalid dependency type: %s", value)
		}
		query += " AND dependency_type = ?"
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Risk); value != "" {
		if !validDependencyRisk(value) {
			return nil, fmt.Errorf("invalid risk: %s", value)
		}
		query += " AND risk = ?"
		args = append(args, value)
	}
	query += " ORDER BY created_at ASC"
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []DependencyRiskRecord
	for rows.Next() {
		record, err := scanDependencyRisk(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func normalizeDependencyRiskInput(input DependencyRiskInput) (DependencyRiskRecord, error) {
	record := DependencyRiskRecord{
		ProjectID:          strings.TrimSpace(input.ProjectID),
		Name:               strings.TrimSpace(input.Name),
		PackageManager:     strings.TrimSpace(input.PackageManager),
		DependencyType:     strings.TrimSpace(input.DependencyType),
		IntroducedByTaskID: strings.TrimSpace(input.IntroducedByTaskID),
		IntroducedByRunID:  strings.TrimSpace(input.IntroducedByRunID),
		DecisionID:         strings.TrimSpace(input.DecisionID),
		Reason:             strings.TrimSpace(input.Reason),
		ApprovedBy:         strings.TrimSpace(input.ApprovedBy),
		Risk:               strings.TrimSpace(input.Risk),
		LockfileChanged:    input.LockfileChanged,
		LifecycleScripts:   strings.TrimSpace(input.LifecycleScripts),
		CurrentVersion:     strings.TrimSpace(input.CurrentVersion),
		ApprovedScope:      strings.TrimSpace(input.ApprovedScope),
		ExpiresAt:          strings.TrimSpace(input.ExpiresAt),
	}
	if record.ProjectID == "" {
		return DependencyRiskRecord{}, fmt.Errorf("project id is required")
	}
	if record.Name == "" {
		return DependencyRiskRecord{}, fmt.Errorf("dependency name is required")
	}
	if !validDependencyPackageManager(record.PackageManager) {
		return DependencyRiskRecord{}, fmt.Errorf("invalid package manager: %s", record.PackageManager)
	}
	if !validLedgerDependencyType(record.DependencyType) {
		return DependencyRiskRecord{}, fmt.Errorf("invalid dependency type: %s", record.DependencyType)
	}
	if record.Reason == "" {
		return DependencyRiskRecord{}, fmt.Errorf("reason is required")
	}
	if !validDependencyRisk(record.Risk) {
		return DependencyRiskRecord{}, fmt.Errorf("invalid risk: %s", record.Risk)
	}
	if record.LifecycleScripts == "" {
		record.LifecycleScripts = "unknown"
	}
	if !validDependencyLifecycleScripts(record.LifecycleScripts) {
		return DependencyRiskRecord{}, fmt.Errorf("invalid lifecycle scripts: %s", record.LifecycleScripts)
	}
	if record.ApprovedScope == "" {
		record.ApprovedScope = "project"
	}
	if !validDependencyApprovedScope(record.ApprovedScope) {
		return DependencyRiskRecord{}, fmt.Errorf("invalid approved scope: %s", record.ApprovedScope)
	}
	if record.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, record.ExpiresAt); err != nil {
			return DependencyRiskRecord{}, fmt.Errorf("expires-at must be RFC3339: %w", err)
		}
	}
	return record, nil
}

func scanDependencyRisk(scanner interface {
	Scan(dest ...any) error
}) (DependencyRiskRecord, error) {
	var record DependencyRiskRecord
	var taskID, runID, decisionID, approvedBy, currentVersion, expiresAt sql.NullString
	var lockfileChanged int
	if err := scanner.Scan(
		&record.ID,
		&record.ProjectID,
		&record.Name,
		&record.PackageManager,
		&record.DependencyType,
		&taskID,
		&runID,
		&decisionID,
		&record.Reason,
		&approvedBy,
		&record.Risk,
		&lockfileChanged,
		&record.LifecycleScripts,
		&currentVersion,
		&record.ApprovedScope,
		&expiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return DependencyRiskRecord{}, err
	}
	record.LockfileChanged = lockfileChanged == 1
	if taskID.Valid {
		record.IntroducedByTaskID = taskID.String
	}
	if runID.Valid {
		record.IntroducedByRunID = runID.String
	}
	if decisionID.Valid {
		record.DecisionID = decisionID.String
	}
	if approvedBy.Valid {
		record.ApprovedBy = approvedBy.String
	}
	if currentVersion.Valid {
		record.CurrentVersion = currentVersion.String
	}
	if expiresAt.Valid {
		record.ExpiresAt = expiresAt.String
	}
	return record, nil
}

func validDependencyPackageManager(value string) bool {
	switch value {
	case "go", "npm", "pnpm", "yarn", "cargo", "other":
		return true
	default:
		return false
	}
}

func validLedgerDependencyType(value string) bool {
	switch value {
	case "production", "development", "tool":
		return true
	default:
		return false
	}
}

func validDependencyRisk(value string) bool {
	switch value {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validDependencyLifecycleScripts(value string) bool {
	switch value {
	case "none_detected", "detected", "unknown":
		return true
	default:
		return false
	}
}

func validDependencyApprovedScope(value string) bool {
	switch value {
	case "project", "task", "one_time", "dependency_family":
		return true
	default:
		return false
	}
}
