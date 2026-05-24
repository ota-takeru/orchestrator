package storage

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var taskYAMLIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]*-[A-Z0-9_-]+$`)

type TaskRecord struct {
	ID                   string                    `json:"id"`
	Status               string                    `json:"status"`
	Title                string                    `json:"title"`
	VerificationCommands []TaskVerificationCommand `json:"verification_commands,omitempty"`
}

type TaskVerificationCommand struct {
	ID               string                         `json:"id"`
	Environment      string                         `json:"environment"`
	Runner           string                         `json:"runner"`
	RequiredForMerge bool                           `json:"required_for_merge"`
	WorkingDir       string                         `json:"working_dir"`
	Command          TaskVerificationCommandCommand `json:"command"`
	Timeout          string                         `json:"timeout,omitempty"`
	Network          bool                           `json:"network"`
}

type TaskVerificationCommandCommand struct {
	Argv []string `json:"argv"`
}

func (db *DB) MaterializeApprovedTasks(ctx context.Context, projectID string) ([]TaskRecord, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	approved, err := approvedArtifactVersions(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	required := []ArtifactType{ArtifactPRD, ArtifactArchitecture, ArtifactRoadmap, ArtifactTaskYAML}
	for _, typ := range required {
		if _, ok := approved[typ]; !ok {
			return nil, fmt.Errorf("approved %s artifact is required", typ)
		}
	}
	projectRoot, err := projectRoot(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	taskArtifact := approved[ArtifactTaskYAML]
	task, err := parseGeneratedTaskYAML(filepath.Join(projectRoot, filepath.FromSlash(taskArtifact.Path)))
	if err != nil {
		return nil, err
	}
	verificationCommands, err := json.Marshal(task.VerificationCommands)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks(
  id, project_id, artifact_version_id, status, title, base_branch, verification_commands_json, created_at, updated_at
) VALUES (?, ?, ?, 'ready', ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  artifact_version_id = excluded.artifact_version_id,
  status = CASE
    WHEN tasks.status = 'proposed' THEN 'ready'
    ELSE tasks.status
  END,
  title = excluded.title,
  base_branch = excluded.base_branch,
  verification_commands_json = excluded.verification_commands_json,
  updated_at = excluded.updated_at`,
		task.ID, projectID, taskArtifact.VersionID, task.Title, task.BaseBranch, string(verificationCommands), now, now,
	); err != nil {
		return nil, err
	}
	queueID, err := enqueueTaskImplementationWorkItem(ctx, tx, projectID, task.ID, now)
	if err != nil {
		return nil, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "tasks_materialized", map[string]any{
		"task_id":             task.ID,
		"artifact_version_id": taskArtifact.VersionID,
		"work_queue_item_id":  queueID,
	}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return []TaskRecord{{ID: task.ID, Status: "ready", Title: task.Title, VerificationCommands: task.VerificationCommands}}, nil
}

func enqueueTaskImplementationWorkItem(ctx context.Context, tx *sql.Tx, projectID string, taskID string, now string) (string, error) {
	queueID := "WQ-" + stableShortHash(projectID+"|task_implementation|"+taskID)
	idempotencyKey := "task_implementation:" + taskID
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, created_at, updated_at
) VALUES (?, ?, 'execution', 'task_implementation', ?, 'queued', 'medium', 0, 3, ?, ?, ?)`,
		queueID, projectID, taskID, idempotencyKey, now, now,
	)
	if err != nil {
		return "", err
	}
	return queueID, nil
}

func (db *DB) EnqueueTaskRepair(ctx context.Context, projectID string, taskID string, causeRunID string) (WorkQueueItemRecord, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(taskID) == "" {
		return WorkQueueItemRecord{}, fmt.Errorf("project id and task id are required")
	}
	if strings.TrimSpace(causeRunID) == "" {
		causeRunID = "unknown"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	queueID := "WQ-" + stableShortHash(projectID+"|task_repair|"+taskID+"|"+causeRunID)
	idempotencyKey := "task_repair:" + taskID + ":" + causeRunID
	if _, err := db.sql.ExecContext(ctx, `
INSERT OR IGNORE INTO work_queue_items(
  id, project_id, lane, item_type, item_id, status, priority,
  attempt_no, max_attempts, idempotency_key, created_at, updated_at
) VALUES (?, ?, 'execution', 'task_repair', ?, 'queued', 'medium', 0, 3, ?, ?, ?)`,
		queueID, projectID, taskID, idempotencyKey, now, now,
	); err != nil {
		return WorkQueueItemRecord{}, err
	}
	items, err := db.ListWorkQueueItems(ctx, projectID, "")
	if err != nil {
		return WorkQueueItemRecord{}, err
	}
	for _, item := range items {
		if item.ID == queueID {
			return item, nil
		}
	}
	return WorkQueueItemRecord{}, fmt.Errorf("repair queue item not found: %s", queueID)
}

func (db *DB) ListTasks(ctx context.Context, projectID string, status string) ([]TaskRecord, error) {
	query := "SELECT id, status, title, verification_commands_json FROM tasks WHERE project_id = ?"
	args := []any{projectID}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY id"
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []TaskRecord
	for rows.Next() {
		var task TaskRecord
		var verificationCommandsJSON string
		if err := rows.Scan(&task.ID, &task.Status, &task.Title, &verificationCommandsJSON); err != nil {
			return nil, err
		}
		if strings.TrimSpace(verificationCommandsJSON) != "" {
			if err := json.Unmarshal([]byte(verificationCommandsJSON), &task.VerificationCommands); err != nil {
				return nil, err
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

type approvedArtifact struct {
	ArtifactID string
	VersionID  string
	Path       string
	Status     string
}

func approvedArtifactVersions(ctx context.Context, tx *sql.Tx, projectID string) (map[ArtifactType]approvedArtifact, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT a.artifact_type, a.id, av.id, av.path, av.status
FROM artifacts a
JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND av.status IN ('approved', 'approved_with_notes')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[ArtifactType]approvedArtifact{}
	for rows.Next() {
		var typ ArtifactType
		var artifact approvedArtifact
		if err := rows.Scan(&typ, &artifact.ArtifactID, &artifact.VersionID, &artifact.Path, &artifact.Status); err != nil {
			return nil, err
		}
		out[typ] = artifact
	}
	return out, rows.Err()
}

func projectRoot(ctx context.Context, tx *sql.Tx, projectID string) (string, error) {
	var root string
	if err := tx.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("project not found: %s", projectID)
		}
		return "", err
	}
	return root, nil
}

type parsedTaskYAML struct {
	ID                   string
	Title                string
	BaseBranch           string
	VerificationCommands []TaskVerificationCommand
}

func parseGeneratedTaskYAML(path string) (parsedTaskYAML, error) {
	file, err := os.Open(path)
	if err != nil {
		return parsedTaskYAML{}, err
	}
	defer file.Close()
	task := parsedTaskYAML{BaseBranch: "main"}
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		rawLine := scanner.Text()
		lines = append(lines, rawLine)
		if strings.TrimSpace(rawLine) != rawLine {
			continue
		}
		line := strings.TrimSpace(rawLine)
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "id":
			task.ID = value
		case "title":
			task.Title = value
		case "base_branch":
			task.BaseBranch = value
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedTaskYAML{}, err
	}
	task.VerificationCommands = parseVerificationCommandsYAML(lines)
	if err := validateParsedTaskYAML(task); err != nil {
		return parsedTaskYAML{}, err
	}
	return task, nil
}

func validateParsedTaskYAML(task parsedTaskYAML) error {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("task yaml requires id and title")
	}
	if !taskYAMLIDPattern.MatchString(task.ID) {
		return fmt.Errorf("task yaml id is invalid: %s", task.ID)
	}
	if strings.TrimSpace(task.BaseBranch) == "" {
		return fmt.Errorf("task yaml requires base_branch")
	}
	seenCommands := map[string]struct{}{}
	for i, command := range task.VerificationCommands {
		if strings.TrimSpace(command.ID) == "" {
			return fmt.Errorf("verification command %d requires id", i)
		}
		if _, ok := seenCommands[command.ID]; ok {
			return fmt.Errorf("verification command id is duplicated: %s", command.ID)
		}
		seenCommands[command.ID] = struct{}{}
		if strings.TrimSpace(command.Environment) == "" {
			return fmt.Errorf("verification command %s requires environment", command.ID)
		}
		if strings.TrimSpace(command.Runner) == "" {
			return fmt.Errorf("verification command %s requires runner", command.ID)
		}
		if strings.TrimSpace(command.WorkingDir) == "" {
			return fmt.Errorf("verification command %s requires working_dir", command.ID)
		}
		if len(command.Command.Argv) == 0 {
			return fmt.Errorf("verification command %s requires command.argv", command.ID)
		}
		for _, arg := range command.Command.Argv {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("verification command %s contains empty argv item", command.ID)
			}
		}
	}
	return nil
}

func parseVerificationCommandsYAML(lines []string) []TaskVerificationCommand {
	commands := []TaskVerificationCommand{}
	inCommands := false
	var current *TaskVerificationCommand
	inCommandBlock := false
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(rawLine, " ") && strings.HasSuffix(trimmed, ":") {
			if trimmed == "verification_commands:" {
				inCommands = true
				continue
			}
			if inCommands {
				break
			}
		}
		if !inCommands {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			if current != nil {
				commands = append(commands, normalizeVerificationCommand(*current))
			}
			current = &TaskVerificationCommand{Environment: "primary", Runner: "auto", WorkingDir: "task_worktree", RequiredForMerge: true}
			inCommandBlock = false
			assignment := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			applyVerificationCommandAssignment(current, assignment, inCommandBlock)
			continue
		}
		if current == nil {
			continue
		}
		if trimmed == "command:" {
			inCommandBlock = true
			continue
		}
		if key, value, ok := strings.Cut(trimmed, ":"); ok {
			assignment := strings.TrimSpace(key) + ":" + strings.TrimSpace(value)
			applyVerificationCommandAssignment(current, assignment, inCommandBlock)
		}
	}
	if current != nil {
		commands = append(commands, normalizeVerificationCommand(*current))
	}
	return commands
}

func applyVerificationCommandAssignment(command *TaskVerificationCommand, assignment string, inCommandBlock bool) {
	key, value, ok := strings.Cut(assignment, ":")
	if !ok {
		return
	}
	key = strings.TrimSpace(key)
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if inCommandBlock && key == "argv" {
		command.Command.Argv = parseArgv(value)
		return
	}
	switch key {
	case "id":
		command.ID = value
	case "environment":
		command.Environment = value
	case "runner":
		command.Runner = value
	case "required_for_merge":
		command.RequiredForMerge = parseYAMLBool(value, true)
	case "working_dir":
		command.WorkingDir = value
	case "timeout":
		command.Timeout = value
	case "network":
		command.Network = parseYAMLBool(value, false)
	case "argv":
		command.Command.Argv = parseArgv(value)
	}
}

func normalizeVerificationCommand(command TaskVerificationCommand) TaskVerificationCommand {
	if command.Environment == "" {
		command.Environment = "primary"
	}
	if command.Runner == "" {
		command.Runner = "auto"
	}
	if command.WorkingDir == "" {
		command.WorkingDir = "task_worktree"
	}
	return command
}

func parseYAMLBool(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseArgv(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var argv []string
	if strings.HasPrefix(value, "[") {
		if err := json.Unmarshal([]byte(value), &argv); err == nil {
			return argv
		}
	}
	return strings.Fields(value)
}
