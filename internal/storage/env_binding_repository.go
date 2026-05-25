package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	fingerprint := envBindingFingerprint(input.ProjectID, key, input.Value)
	root, err := db.projectRootForEnvBinding(ctx, input.ProjectID)
	if err != nil {
		return EnvBindingRecord{}, err
	}
	if err := appendEnvLocalBinding(root, key, input.Value); err != nil {
		return EnvBindingRecord{}, err
	}
	storageRef := "env_file:.env.local#" + key
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
) VALUES (?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), 'env_file', ?, 'configured', ?, ?, 'human', ?, ?)
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
		Storage:          "env_file",
		StorageRef:       storageRef,
		Status:           "configured",
		RedactedPreview:  redactedPreview,
		ValueFingerprint: fingerprint,
		CreatedBy:        "human",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (db *DB) projectRootForEnvBinding(ctx context.Context, projectID string) (string, error) {
	var root sql.NullString
	if err := db.sql.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("project not found: %s", projectID)
		}
		return "", err
	}
	if !root.Valid || strings.TrimSpace(root.String) == "" {
		return "", fmt.Errorf("project root is required for env file binding")
	}
	return root.String, nil
}

func appendEnvLocalBinding(projectRoot string, key string, value string) error {
	path := filepath.Join(projectRoot, ".env.local")
	if envLocalTracked(projectRoot) {
		return fmt.Errorf(".env.local is tracked by git; refusing to write secret material")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := replaceEnvLine(string(existing), key, key+"="+quoteEnvValue(value))
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func envBindingFingerprint(projectID string, key string, value string) string {
	mac := hmac.New(sha256.New, []byte("devos-env-binding-v1:"+projectID+":"+key))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func envLocalTracked(projectRoot string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", ".env.local")
	cmd.Dir = projectRoot
	return cmd.Run() == nil
}

func replaceEnvLine(content string, key string, replacement string) string {
	prefix := key + "="
	var lines []string
	replaced := false
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			if !replaced {
				lines = append(lines, replacement)
				replaced = true
			}
			continue
		}
		lines = append(lines, line)
	}
	if !replaced {
		lines = append(lines, replacement)
	}
	return strings.Join(lines, "\n") + "\n"
}

func quoteEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\r\n\"'\\#$") {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(value)
	return `"` + escaped + `"`
}
