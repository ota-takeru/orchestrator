package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type MemoryRecord struct {
	ID          string `json:"id"`
	MemoryType  string `json:"memory_type"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Scope       string `json:"scope"`
	ScopeID     string `json:"scope_id,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	SourceType  string `json:"source_type"`
	SourceID    string `json:"source_id,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Invalidated string `json:"invalidated_at,omitempty"`
}

type RememberDecisionInput struct {
	Key       string
	Scope     string
	ScopeID   string
	ExpiresAt string
}

func (db *DB) ListMemories(ctx context.Context, projectID string, memoryType string) ([]MemoryRecord, error) {
	query := `
SELECT id, memory_type, key, value, scope, scope_id, expires_at, source_type, source_id,
       created_at, updated_at, invalidated_at
FROM memories
WHERE project_id = ?`
	args := []any{projectID}
	if strings.TrimSpace(memoryType) != "" {
		query += " AND memory_type = ?"
		args = append(args, memoryType)
	}
	query += " ORDER BY created_at ASC"
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []MemoryRecord
	for rows.Next() {
		record, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func rememberApprovedDecision(ctx context.Context, tx *sql.Tx, projectID string, decision DecisionRecord, input DecisionApprovalInput, now string) error {
	remember := input.Remember
	if !remember {
		return nil
	}
	key := strings.TrimSpace(input.Memory.Key)
	if key == "" {
		return fmt.Errorf("memory key is required when remember is enabled")
	}
	scope := strings.TrimSpace(input.Memory.Scope)
	if scope == "" {
		scope = "project"
	}
	if !validMemoryScope(scope) {
		return fmt.Errorf("invalid memory scope: %s", scope)
	}
	scopeID := strings.TrimSpace(input.Memory.ScopeID)
	valueJSON, err := json.Marshal(map[string]any{
		"decision_id":     input.DecisionID,
		"title":           decision.Title,
		"selected_option": input.Option,
		"notes":           strings.TrimSpace(input.Notes),
	})
	if err != nil {
		return err
	}
	memoryID := "MEM-" + stableShortHash(projectID+"|policy|"+key+"|"+scope+"|"+scopeID)
	_, err = tx.ExecContext(ctx, `
INSERT INTO memories(
  id, project_id, memory_type, key, value, scope, scope_id, expires_at,
  source_type, source_id, created_at, updated_at
) VALUES (?, ?, 'policy', ?, ?, ?, ?, ?, 'human_decision', ?, ?, ?)
ON CONFLICT(project_id, memory_type, key, scope, scope_id) DO UPDATE SET
  value = excluded.value,
  expires_at = excluded.expires_at,
  invalidated_at = NULL,
  invalidated_by_change_request_id = NULL,
  source_type = excluded.source_type,
  source_id = excluded.source_id,
  updated_at = excluded.updated_at`,
		memoryID, projectID, key, string(valueJSON), scope, scopeID, nullableText(input.Memory.ExpiresAt), input.DecisionID, now, now,
	)
	return err
}

func scanMemory(scanner interface {
	Scan(dest ...any) error
}) (MemoryRecord, error) {
	var record MemoryRecord
	var scopeID, expiresAt, sourceID, invalidatedAt sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.MemoryType,
		&record.Key,
		&record.Value,
		&record.Scope,
		&scopeID,
		&expiresAt,
		&record.SourceType,
		&sourceID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&invalidatedAt,
	); err != nil {
		return MemoryRecord{}, err
	}
	if scopeID.Valid {
		record.ScopeID = scopeID.String
	}
	if expiresAt.Valid {
		record.ExpiresAt = expiresAt.String
	}
	if sourceID.Valid {
		record.SourceID = sourceID.String
	}
	if invalidatedAt.Valid {
		record.Invalidated = invalidatedAt.String
	}
	return record, nil
}

func validMemoryScope(scope string) bool {
	switch scope {
	case "project", "task", "dependency_family", "one_time", "user_default":
		return true
	default:
		return false
	}
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
