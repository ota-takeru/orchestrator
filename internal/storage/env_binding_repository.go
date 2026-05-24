package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type EnvBindingInput struct {
	ProjectID     string
	EnvironmentID string
	Key           string
	Scope         string
	ScopeID       string
	Value         string
}

type EnvBindingRecord struct {
	ID               string `json:"id"`
	EnvironmentID    string `json:"environment_id,omitempty"`
	Key              string `json:"key"`
	Scope            string `json:"scope"`
	ScopeID          string `json:"scope_id,omitempty"`
	Storage          string `json:"storage"`
	StorageRef       string `json:"storage_ref"`
	Status           string `json:"status"`
	RedactedPreview  string `json:"redacted_preview"`
	ValueFingerprint string `json:"value_fingerprint"`
	CreatedBy        string `json:"created_by"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func (db *DB) SaveEnvBinding(ctx context.Context, input EnvBindingInput) (EnvBindingRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return EnvBindingRecord{}, fmt.Errorf("project id is required")
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return EnvBindingRecord{}, fmt.Errorf("environment key is required")
	}
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "task" && scope != "run" && scope != "user_default" {
		return EnvBindingRecord{}, fmt.Errorf("unsupported env scope: %s", scope)
	}
	if input.Value == "" {
		return EnvBindingRecord{}, fmt.Errorf("environment value is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	environmentID := strings.TrimSpace(input.EnvironmentID)
	scopeID := strings.TrimSpace(input.ScopeID)
	bindingID := "ENV-BIND-" + stableShortHash(input.ProjectID+"|"+environmentID+"|"+key+"|"+scope+"|"+scopeID)
	fingerprint := sha256Hex([]byte(input.Value))
	storageRef := "redacted://" + bindingID
	redactedPreview := "configured"

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return EnvBindingRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO environment_bindings(
  id, project_id, environment_id, key, scope, scope_id, storage, storage_ref,
  status, redacted_preview, value_fingerprint, created_by, created_at, updated_at
) VALUES (?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), 'external_secret', ?, 'configured', ?, ?, 'human', ?, ?)
ON CONFLICT(id) DO UPDATE SET
  environment_id = excluded.environment_id,
  storage_ref = excluded.storage_ref,
  status = excluded.status,
  redacted_preview = excluded.redacted_preview,
  value_fingerprint = excluded.value_fingerprint,
  updated_at = excluded.updated_at`,
		bindingID, input.ProjectID, environmentID, key, scope, scopeID, storageRef, redactedPreview, fingerprint, now, now,
	); err != nil {
		return EnvBindingRecord{}, err
	}
	auditID := "ENV-AUDIT-" + stableShortHash(bindingID+"|configured|"+now)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO environment_audit_events(
  id, project_id, environment_id, binding_id, key, action, actor,
  scope, scope_id, redacted_preview, created_at
) VALUES (?, ?, NULLIF(?, ''), ?, ?, 'configured', 'human', ?, NULLIF(?, ''), ?, ?)`,
		auditID, input.ProjectID, environmentID, bindingID, key, scope, scopeID, redactedPreview, now,
	); err != nil {
		return EnvBindingRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnvBindingRecord{}, err
	}
	committed = true
	return EnvBindingRecord{
		ID:               bindingID,
		EnvironmentID:    environmentID,
		Key:              key,
		Scope:            scope,
		ScopeID:          scopeID,
		Storage:          "external_secret",
		StorageRef:       storageRef,
		Status:           "configured",
		RedactedPreview:  redactedPreview,
		ValueFingerprint: fingerprint,
		CreatedBy:        "human",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}
